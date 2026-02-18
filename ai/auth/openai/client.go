package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultIssuerBaseURL = "https://auth.openai.com"
	DefaultClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
)

type OAuthClient struct {
	HTTPClient        *http.Client
	IssuerBaseURL     string
	ClientID          string
	DeviceFlowTimeout time.Duration
}

func NewOAuthClient() *OAuthClient {
	return &OAuthClient{
		HTTPClient: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *OAuthClient) StartDeviceFlow(ctx context.Context) (*DeviceCode, error) {
	type userCodeRequest struct {
		ClientID string `json:"client_id"`
	}
	type userCodeResponse struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		UserCodeAlt  string `json:"usercode"`
		Interval     any    `json:"interval"`
	}

	body, err := json.Marshal(userCodeRequest{ClientID: c.clientID()})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.apiAccountsBaseURL()+"/deviceauth/usercode",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readStatusError("device code request failed", resp)
	}

	var parsed userCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	userCode := strings.TrimSpace(parsed.UserCode)
	if userCode == "" {
		userCode = strings.TrimSpace(parsed.UserCodeAlt)
	}
	if strings.TrimSpace(parsed.DeviceAuthID) == "" || userCode == "" {
		return nil, errors.New("device code response missing device_auth_id or user_code")
	}

	interval := parseSeconds(parsed.Interval)
	if interval <= 0 {
		interval = 5
	}

	return &DeviceCode{
		DeviceCode:      strings.TrimSpace(parsed.DeviceAuthID),
		UserCode:        userCode,
		VerificationURI: c.issuerBaseURL() + "/codex/device",
		IntervalSeconds: interval,
	}, nil
}

func (c *OAuthClient) PollDeviceFlow(ctx context.Context, code *DeviceCode) (*Credentials, error) {
	if code == nil {
		return nil, errors.New("device code is required")
	}
	if strings.TrimSpace(code.DeviceCode) == "" || strings.TrimSpace(code.UserCode) == "" {
		return nil, errors.New("device code and user code are required")
	}

	type tokenPollRequest struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
	}
	type tokenPollResponse struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}

	timeout := c.deviceFlowTimeout()
	deadline := time.Now().Add(timeout)
	interval := time.Duration(code.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}

	for {
		body, err := json.Marshal(tokenPollRequest{
			DeviceAuthID: code.DeviceCode,
			UserCode:     code.UserCode,
		})
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			c.apiAccountsBaseURL()+"/deviceauth/token",
			bytes.NewReader(body),
		)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var parsed tokenPollResponse
			decodeErr := json.NewDecoder(resp.Body).Decode(&parsed)
			resp.Body.Close()
			if decodeErr != nil {
				return nil, decodeErr
			}
			if strings.TrimSpace(parsed.AuthorizationCode) == "" || strings.TrimSpace(parsed.CodeVerifier) == "" {
				return nil, errors.New("device auth token response missing authorization_code or code_verifier")
			}
			return c.exchangeAuthorizationCode(ctx, parsed.AuthorizationCode, parsed.CodeVerifier)
		}

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			if time.Now().After(deadline) {
				return nil, errors.New("device auth timed out after waiting for approval")
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(interval):
				continue
			}
		}

		err = readStatusError("device auth failed", resp)
		resp.Body.Close()
		return nil, err
	}
}

func (c *OAuthClient) Refresh(ctx context.Context, refreshToken string) (*Credentials, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, errors.New("refresh token is required")
	}

	reqBody := map[string]any{
		"client_id":     c.clientID(),
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"scope":         "openid profile email",
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.issuerBaseURL()+"/oauth/token",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readStatusError("refresh token request failed", resp)
	}

	var parsed oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return nil, errors.New("refresh response missing access_token")
	}

	creds := credentialsFromOAuthTokenResponse(parsed)
	if strings.TrimSpace(creds.RefreshToken) == "" {
		creds.RefreshToken = refreshToken
	}
	return creds, nil
}

func (c *OAuthClient) exchangeAuthorizationCode(
	ctx context.Context,
	authorizationCode string,
	codeVerifier string,
) (*Credentials, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", authorizationCode)
	values.Set("redirect_uri", c.issuerBaseURL()+"/deviceauth/callback")
	values.Set("client_id", c.clientID())
	values.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.issuerBaseURL()+"/oauth/token",
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readStatusError("authorization code exchange failed", resp)
	}

	var parsed oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if strings.TrimSpace(parsed.AccessToken) == "" || strings.TrimSpace(parsed.RefreshToken) == "" {
		return nil, errors.New("authorization code exchange response missing access_token or refresh_token")
	}
	return credentialsFromOAuthTokenResponse(parsed), nil
}

type oauthTokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    any    `json:"expires_in"`
}

func credentialsFromOAuthTokenResponse(parsed oauthTokenResponse) *Credentials {
	creds := &Credentials{
		AccessToken:  strings.TrimSpace(parsed.AccessToken),
		RefreshToken: strings.TrimSpace(parsed.RefreshToken),
		AccountID:    extractAccountIDFromJWT(parsed.IDToken),
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	if creds.AccountID == "" {
		creds.AccountID = extractAccountIDFromJWT(parsed.AccessToken)
	}

	if expiresIn := parseSeconds(parsed.ExpiresIn); expiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
		return creds
	}
	if expiry, ok := extractJWTExpiry(parsed.AccessToken); ok {
		creds.ExpiresAt = expiry
	}

	return creds
}

func extractAccountIDFromJWT(token string) string {
	claims := extractJWTAuthClaims(token)
	accountID, _ := claims["chatgpt_account_id"].(string)
	return strings.TrimSpace(accountID)
}

func extractJWTExpiry(token string) (time.Time, bool) {
	payload, ok := decodeJWTPayload(token)
	if !ok {
		return time.Time{}, false
	}
	expFloat, ok := payload["exp"].(float64)
	if !ok || expFloat <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(expFloat), 0), true
}

func extractJWTAuthClaims(token string) map[string]any {
	payload, ok := decodeJWTPayload(token)
	if !ok {
		return map[string]any{}
	}
	auth, ok := payload["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return auth
}

func decodeJWTPayload(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, false
	}
	bytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func parseSeconds(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case float32:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i)
		}
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return i
		}
	}
	return 0
}

func readStatusError(prefix string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Errorf("%s: status=%d", prefix, resp.StatusCode)
	}
	return fmt.Errorf("%s: status=%d body=%s", prefix, resp.StatusCode, text)
}

func (c *OAuthClient) httpClient() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 45 * time.Second}
}

func (c *OAuthClient) issuerBaseURL() string {
	if c == nil {
		return DefaultIssuerBaseURL
	}
	base := strings.TrimSpace(c.IssuerBaseURL)
	if base == "" {
		return DefaultIssuerBaseURL
	}
	return strings.TrimRight(base, "/")
}

func (c *OAuthClient) apiAccountsBaseURL() string {
	return c.issuerBaseURL() + "/api/accounts"
}

func (c *OAuthClient) clientID() string {
	if c == nil {
		return DefaultClientID
	}
	id := strings.TrimSpace(c.ClientID)
	if id == "" {
		return DefaultClientID
	}
	return id
}

func (c *OAuthClient) deviceFlowTimeout() time.Duration {
	if c != nil && c.DeviceFlowTimeout > 0 {
		return c.DeviceFlowTimeout
	}
	return 15 * time.Minute
}
