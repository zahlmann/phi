package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/stream"
)

var errSSEDone = errors.New("sse done")

const defaultChatGPTBackendBaseURL = "https://chatgpt.com/backend-api/codex"

type OpenAIClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewOpenAIClient() *OpenAIClient {
	return &OpenAIClient{
		BaseURL: "https://api.openai.com/v1",
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *OpenAIClient) Stream(
	ctx context.Context,
	m model.Model,
	conversation model.Context,
	options StreamOptions,
) (stream.EventStream, error) {
	if m.ID == "" {
		return nil, errors.New("model id is required")
	}

	switch normalizeAuthMode(options.AuthMode) {
	case AuthModeChatGPT:
		return c.streamChatGPTBackend(ctx, m, conversation, options)
	default:
		apiKey := strings.TrimSpace(options.APIKey)
		if apiKey == "" {
			apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		}
		if apiKey == "" {
			return nil, errors.New("openai api key is required")
		}

		baseURL := strings.TrimRight(options.BaseURL, "/")
		if baseURL == "" {
			baseURL = strings.TrimRight(c.BaseURL, "/")
		}
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		return c.streamOpenAIResponsesAPI(ctx, m, conversation, options, apiKey, baseURL)
	}
}

func (c *OpenAIClient) streamOpenAIResponsesAPI(
	ctx context.Context,
	m model.Model,
	conversation model.Context,
	options StreamOptions,
	apiKey string,
	baseURL string,
) (stream.EventStream, error) {
	request := buildChatGPTResponsesRequest(m, conversation, options)
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithCancel(ctx)
	url := strings.TrimRight(baseURL, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range options.Headers {
		httpReq.Header.Set(k, v)
	}

	client := streamingHTTPClient(c.HTTPClient)
	resp, err := client.Do(httpReq)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("openai request send failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("openai request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/event-stream") {
		parsed, parseErr := parseChatGPTNonStreamingResponse(resp, m, "openai")
		cancel()
		return parsed, parseErr
	}

	return newChatGPTResponsesEventStream(reqCtx, cancel, resp, m, "openai"), nil
}

func (c *OpenAIClient) streamChatGPTBackend(
	ctx context.Context,
	m model.Model,
	conversation model.Context,
	options StreamOptions,
) (stream.EventStream, error) {
	accessToken, accountID, err := resolveChatGPTAuth(ctx, options)
	if err != nil {
		return nil, err
	}

	request := buildChatGPTResponsesRequest(m, conversation, options)
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	baseURL := normalizeChatGPTBaseURL(options.BaseURL, c.BaseURL)
	endpoint := chatGPTResponsesEndpoint(baseURL)

	reqCtx, cancel := context.WithCancel(ctx)
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if strings.TrimSpace(accountID) != "" {
		httpReq.Header.Set("ChatGPT-Account-ID", strings.TrimSpace(accountID))
	}
	for k, v := range options.Headers {
		httpReq.Header.Set(k, v)
	}

	client := streamingHTTPClient(c.HTTPClient)
	resp, err := client.Do(httpReq)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("chatgpt backend request send failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf(
			"chatgpt backend request failed: status=%d body=%s",
			resp.StatusCode,
			string(body),
		)
	}

	return newChatGPTResponsesEventStream(reqCtx, cancel, resp, m, "chatgpt"), nil
}

func normalizeAuthMode(mode AuthMode) AuthMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case string(AuthModeChatGPT):
		return AuthModeChatGPT
	default:
		return AuthModeOpenAIAPIKey
	}
}

func normalizeReasoningEffort(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "none", "minimal", "low", "medium", "high", "xhigh":
		return value
	default:
		return ""
	}
}

func resolveChatGPTAuth(ctx context.Context, options StreamOptions) (string, string, error) {
	_ = ctx
	accessToken := strings.TrimSpace(options.AccessToken)
	accountID := strings.TrimSpace(options.AccountID)

	if accessToken == "" {
		accessToken = strings.TrimSpace(os.Getenv("PHI_CHATGPT_ACCESS_TOKEN"))
		accountID = strings.TrimSpace(os.Getenv("PHI_CHATGPT_ACCOUNT_ID"))
	}

	if accessToken == "" {
		accessToken, accountID = loadChatGPTAuthFromHome(".phi", "chatgpt_tokens.json")
	}

	if accessToken == "" {
		accessToken, accountID = loadCodexChatGPTAuthFromHome()
	}

	if accessToken == "" {
		return "", "", errors.New(
			"chatgpt access token is required (set StreamOptions.AccessToken, PHI_CHATGPT_ACCESS_TOKEN, ~/.phi/chatgpt_tokens.json, or ~/.codex/auth.json)",
		)
	}
	if accountID == "" {
		accountID = extractChatGPTAccountIDFromJWT(accessToken)
	}
	return accessToken, accountID, nil
}

type chatGPTTokenFile struct {
	AccessToken      string `json:"accessToken"`
	AccountID        string `json:"accountId"`
	AccessTokenSnake string `json:"access_token"`
	AccountIDSnake   string `json:"account_id"`
}

func loadChatGPTAuthFromHome(pathParts ...string) (string, string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	path := filepath.Join(append([]string{homeDir}, pathParts...)...)
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}

	var decoded chatGPTTokenFile
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", ""
	}

	accessToken := strings.TrimSpace(decoded.AccessToken)
	if accessToken == "" {
		accessToken = strings.TrimSpace(decoded.AccessTokenSnake)
	}
	accountID := strings.TrimSpace(decoded.AccountID)
	if accountID == "" {
		accountID = strings.TrimSpace(decoded.AccountIDSnake)
	}
	return accessToken, accountID
}

type codexAuthFile struct {
	Tokens struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

func loadCodexChatGPTAuthFromHome() (string, string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	path := filepath.Join(homeDir, ".codex", "auth.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}

	var decoded codexAuthFile
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", ""
	}

	return strings.TrimSpace(decoded.Tokens.AccessToken), strings.TrimSpace(decoded.Tokens.AccountID)
}

func normalizeChatGPTBaseURL(optionBaseURL string, clientBaseURL string) string {
	baseURL := strings.TrimSpace(optionBaseURL)
	if baseURL == "" && isChatGPTBaseURL(strings.TrimSpace(clientBaseURL)) {
		baseURL = strings.TrimSpace(clientBaseURL)
	}
	if baseURL == "" {
		return defaultChatGPTBackendBaseURL
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if (strings.HasPrefix(baseURL, "https://chatgpt.com") ||
		strings.HasPrefix(baseURL, "https://chat.openai.com")) &&
		!strings.Contains(baseURL, "/backend-api") {
		baseURL += "/backend-api/codex"
	}
	if strings.HasSuffix(baseURL, "/backend-api") {
		baseURL += "/codex"
	}
	return baseURL
}

func chatGPTResponsesEndpoint(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/responses") {
		return baseURL
	}
	return baseURL + "/responses"
}

func isChatGPTBaseURL(baseURL string) bool {
	baseURL = strings.TrimSpace(baseURL)
	return strings.HasPrefix(baseURL, "https://chatgpt.com") ||
		strings.HasPrefix(baseURL, "https://chat.openai.com")
}

func streamingHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{}
	}
	if client.Timeout == 0 {
		return client
	}

	copy := *client
	copy.Timeout = 0
	return &copy
}

func extractToolCalls(content []any) []model.ToolCallContent {
	out := []model.ToolCallContent{}
	for i, item := range content {
		call := model.ToolCallContent{Type: model.ContentToolCall}
		found := false

		switch v := item.(type) {
		case model.ToolCallContent:
			call = v
			call.Type = model.ContentToolCall
			found = true
		case map[string]any:
			kind, _ := v["type"].(string)
			if kind == string(model.ContentToolCall) {
				call.ID, _ = v["id"].(string)
				call.Name, _ = v["name"].(string)
				if args, ok := v["arguments"].(map[string]any); ok {
					call.Arguments = args
				} else {
					call.Arguments = map[string]any{}
				}
				found = true
			}
		}

		if !found {
			continue
		}
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", i+1)
		}
		if call.Name == "" {
			call.Name = "tool"
		}
		if call.Arguments == nil {
			call.Arguments = map[string]any{}
		}
		out = append(out, call)
	}
	return out
}

func extractText(content []any) string {
	parts := []string{}
	for _, item := range content {
		switch v := item.(type) {
		case model.TextContent:
			if strings.TrimSpace(v.Text) != "" {
				parts = append(parts, v.Text)
			}
		case map[string]any:
			kind, _ := v["type"].(string)
			if kind == string(model.ContentText) {
				text, _ := v["text"].(string)
				if strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

type openAIEventItem struct {
	event stream.Event
	err   error
}

type openAIResultItem struct {
	msg *model.AssistantMessage
	err error
}

func consumeSSE(body io.Reader, onData func(payload string) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		return onData(payload)
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(trimmed, ":") {
			continue
		}
		if strings.HasPrefix(trimmed, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func parseToolArguments(raw string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
		return out
	}

	var anyValue any
	if err := json.Unmarshal([]byte(trimmed), &anyValue); err == nil {
		return map[string]any{"value": anyValue}
	}

	return map[string]any{"_raw": trimmed}
}

type chatGPTResponsesRequest struct {
	Model             string              `json:"model"`
	Instructions      string              `json:"instructions,omitempty"`
	Input             []any               `json:"input"`
	Tools             []map[string]any    `json:"tools,omitempty"`
	ToolChoice        string              `json:"tool_choice,omitempty"`
	ParallelToolCalls bool                `json:"parallel_tool_calls,omitempty"`
	Reasoning         *responsesReasoning `json:"reasoning,omitempty"`
	Store             bool                `json:"store"`
	Stream            bool                `json:"stream"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

func buildChatGPTResponsesRequest(
	m model.Model,
	conversation model.Context,
	options StreamOptions,
) chatGPTResponsesRequest {
	req := chatGPTResponsesRequest{
		Model:        m.ID,
		Instructions: strings.TrimSpace(conversation.SystemPrompt),
		Input:        toResponsesInput(conversation.Messages),
		Store:        false,
		Stream:       true,
	}
	if effort := normalizeReasoningEffort(options.ReasoningEffort); effort != "" {
		req.Reasoning = &responsesReasoning{Effort: effort}
	}
	if len(conversation.Tools) > 0 {
		req.Tools = convertResponsesTools(conversation.Tools)
		req.ToolChoice = "auto"
		req.ParallelToolCalls = true
	}
	return req
}

func convertResponsesTools(tools []model.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		item := map[string]any{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		}
		out = append(out, item)
	}
	return out
}

func toResponsesInput(messages []model.Message) []any {
	out := []any{}
	for _, msg := range messages {
		switch msg.Role {
		case model.RoleUser:
			content := []map[string]any{}
			for _, item := range msg.ContentRaw {
				switch v := item.(type) {
				case model.TextContent:
					if strings.TrimSpace(v.Text) != "" {
						content = append(content, map[string]any{
							"type": "input_text",
							"text": v.Text,
						})
					}
				case model.ImageContent:
					if strings.TrimSpace(v.Data) != "" {
						content = append(content, map[string]any{
							"type":      "input_image",
							"image_url": "data:" + v.MIMEType + ";base64," + v.Data,
						})
					}
				case map[string]any:
					kind, _ := v["type"].(string)
					switch kind {
					case string(model.ContentText):
						text, _ := v["text"].(string)
						if strings.TrimSpace(text) != "" {
							content = append(content, map[string]any{
								"type": "input_text",
								"text": text,
							})
						}
					case string(model.ContentImage):
						mime, _ := v["mimeType"].(string)
						data, _ := v["data"].(string)
						if strings.TrimSpace(data) != "" {
							content = append(content, map[string]any{
								"type":      "input_image",
								"image_url": "data:" + mime + ";base64," + data,
							})
						}
					}
				}
			}
			if len(content) > 0 {
				out = append(out, map[string]any{
					"type":    "message",
					"role":    "user",
					"content": content,
				})
			}
		case model.RoleAssistant:
			text := extractText(msg.ContentRaw)
			if strings.TrimSpace(text) != "" {
				out = append(out, map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{
							"type": "output_text",
							"text": text,
						},
					},
				})
			}

			for i, call := range extractToolCalls(msg.ContentRaw) {
				callID := strings.TrimSpace(call.ID)
				if callID == "" {
					callID = fmt.Sprintf("call_%d", i+1)
				}
				name := strings.TrimSpace(call.Name)
				if name == "" {
					name = "tool"
				}
				argsBytes, _ := json.Marshal(call.Arguments)
				args := strings.TrimSpace(string(argsBytes))
				if args == "" {
					args = "{}"
				}
				out = append(out, map[string]any{
					"type":      "function_call",
					"call_id":   callID,
					"name":      name,
					"arguments": args,
				})
			}
		case model.RoleToolResult:
			if strings.TrimSpace(msg.ToolCallID) == "" {
				continue
			}
			text := extractText(msg.ContentRaw)
			if strings.TrimSpace(text) == "" {
				text = "(no content)"
			}
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": msg.ToolCallID,
				"output":  text,
			})
		}
	}
	return out
}

type chatGPTResponsesEventStream struct {
	events    chan openAIEventItem
	result    chan openAIResultItem
	provider  string
	closeFn   func()
	closeOnce sync.Once
}

type chatGPTResponsesSSEEvent struct {
	Type     string         `json:"type"`
	Delta    string         `json:"delta"`
	Item     map[string]any `json:"item"`
	Response map[string]any `json:"response"`
}

type chatGPTResponsesAggregation struct {
	requestModel  model.Model
	provider      string
	responseModel string
	text          strings.Builder
	toolCalls     []model.ToolCallContent
	seenToolCall  map[string]bool
	usage         model.Usage
	stopReason    model.StopReason
	completed     bool
}

func (a *chatGPTResponsesAggregation) hasOutput() bool {
	return strings.TrimSpace(a.text.String()) != "" || len(a.toolCalls) > 0
}

func newChatGPTResponsesEventStream(
	ctx context.Context,
	cancel context.CancelFunc,
	resp *http.Response,
	m model.Model,
	provider string,
) *chatGPTResponsesEventStream {
	s := &chatGPTResponsesEventStream{
		events:   make(chan openAIEventItem, 64),
		result:   make(chan openAIResultItem, 1),
		provider: strings.TrimSpace(provider),
		closeFn: func() {
			cancel()
			_ = resp.Body.Close()
		},
	}
	go s.consume(ctx, resp, m)
	return s
}

func (s *chatGPTResponsesEventStream) Recv() (stream.Event, error) {
	item, ok := <-s.events
	if !ok {
		return stream.Event{}, io.EOF
	}
	if item.err != nil {
		return stream.Event{}, item.err
	}
	return item.event, nil
}

func (s *chatGPTResponsesEventStream) Result() (*model.AssistantMessage, error) {
	item, ok := <-s.result
	if !ok {
		return nil, errors.New("stream result unavailable")
	}
	return item.msg, item.err
}

func (s *chatGPTResponsesEventStream) Close() error {
	s.closeOnce.Do(s.closeFn)
	return nil
}

func (s *chatGPTResponsesEventStream) consume(ctx context.Context, resp *http.Response, m model.Model) {
	defer close(s.events)
	defer close(s.result)
	defer resp.Body.Close()

	agg := &chatGPTResponsesAggregation{
		requestModel: m,
		provider:     s.provider,
		seenToolCall: map[string]bool{},
		stopReason:   model.StopReasonStop,
	}
	if agg.provider == "" {
		agg.provider = "chatgpt"
	}
	s.pushEvent(stream.Event{Type: stream.EventStart})

	err := consumeSSE(resp.Body, func(payload string) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if payload == "[DONE]" {
			return errSSEDone
		}

		var event chatGPTResponsesSSEEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return err
		}
		return agg.applyEvent(event, s.pushEvent)
	})
	if err != nil && !errors.Is(err, errSSEDone) {
		if agg.completed {
			err = nil
		}
		if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled") {
			if agg.hasOutput() {
				agg.completed = true
				err = nil
			}
		}
	}
	if err != nil && !errors.Is(err, errSSEDone) {
		s.pushEvent(stream.Event{
			Type:  stream.EventError,
			Error: err.Error(),
		})
		s.result <- openAIResultItem{err: err}
		return
	}
	if !agg.completed {
		err := errors.New("stream closed before response.completed")
		s.pushEvent(stream.Event{
			Type:  stream.EventError,
			Error: err.Error(),
		})
		s.result <- openAIResultItem{err: err}
		return
	}

	assistant := agg.buildAssistant()
	s.pushEvent(stream.Event{
		Type:   stream.EventDone,
		Reason: assistant.StopReason,
	})
	s.result <- openAIResultItem{msg: assistant}
}

func (s *chatGPTResponsesEventStream) pushEvent(event stream.Event) {
	s.events <- openAIEventItem{event: event}
}

func (a *chatGPTResponsesAggregation) applyEvent(
	event chatGPTResponsesSSEEvent,
	emit func(stream.Event),
) error {
	switch event.Type {
	case "response.output_text.delta":
		if strings.TrimSpace(event.Delta) != "" {
			a.text.WriteString(event.Delta)
			emit(stream.Event{
				Type:  stream.EventTextDelta,
				Delta: event.Delta,
			})
		}
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		if strings.TrimSpace(event.Delta) != "" {
			emit(stream.Event{
				Type:  stream.EventThinkingDelta,
				Delta: event.Delta,
			})
		}
	case "response.output_item.done":
		a.handleOutputItemDone(event.Item, emit)
	case "response.failed":
		return errors.New(extractResponsesErrorMessage(event.Response))
	case "response.completed", "response.done":
		a.completed = true
		a.updateFromResponse(event.Response)
	}
	return nil
}

func (a *chatGPTResponsesAggregation) handleOutputItemDone(
	item map[string]any,
	emit func(stream.Event),
) {
	if len(item) == 0 {
		return
	}
	itemType, _ := item["type"].(string)
	if itemType != "function_call" {
		return
	}

	callID, _ := item["call_id"].(string)
	callID = strings.TrimSpace(callID)
	if callID == "" {
		callID = fmt.Sprintf("call_%d", len(a.toolCalls)+1)
	}
	if a.seenToolCall[callID] {
		return
	}

	name, _ := item["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "tool"
	}

	rawArgs, _ := item["arguments"].(string)
	args := parseToolArguments(rawArgs)
	call := model.ToolCallContent{
		Type:      model.ContentToolCall,
		ID:        callID,
		Name:      name,
		Arguments: args,
	}
	a.seenToolCall[callID] = true
	a.toolCalls = append(a.toolCalls, call)
	a.stopReason = model.StopReasonToolUse

	emit(stream.Event{
		Type:       stream.EventToolCall,
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Arguments:  call.Arguments,
	})
}

func (a *chatGPTResponsesAggregation) updateFromResponse(response map[string]any) {
	if len(response) == 0 {
		return
	}

	if modelID, ok := response["model"].(string); ok && strings.TrimSpace(modelID) != "" {
		a.responseModel = strings.TrimSpace(modelID)
	}

	usageRaw, ok := response["usage"].(map[string]any)
	if !ok {
		return
	}
	a.usage = model.Usage{
		Input:  intFromAny(usageRaw["input_tokens"]),
		Output: intFromAny(usageRaw["output_tokens"]),
		Total:  intFromAny(usageRaw["total_tokens"]),
	}
}

func (a *chatGPTResponsesAggregation) buildAssistant() *model.AssistantMessage {
	content := []any{}
	if text := strings.TrimSpace(a.text.String()); text != "" {
		content = append(content, model.TextContent{
			Type: model.ContentText,
			Text: text,
		})
	}
	for _, call := range a.toolCalls {
		content = append(content, call)
	}

	modelID := a.responseModel
	if modelID == "" {
		modelID = a.requestModel.ID
	}

	if len(a.toolCalls) > 0 {
		a.stopReason = model.StopReasonToolUse
	}

	return &model.AssistantMessage{
		Role:       model.RoleAssistant,
		ContentRaw: content,
		Provider:   a.provider,
		Model:      modelID,
		StopReason: a.stopReason,
		Usage:      a.usage,
		Timestamp:  time.Now().UnixMilli(),
	}
}

func parseChatGPTNonStreamingResponse(
	resp *http.Response,
	requestModel model.Model,
	provider string,
) (stream.EventStream, error) {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	response := payload
	if nested, ok := payload["response"].(map[string]any); ok {
		response = nested
	}

	agg := &chatGPTResponsesAggregation{
		requestModel: requestModel,
		provider:     strings.TrimSpace(provider),
		seenToolCall: map[string]bool{},
		stopReason:   model.StopReasonStop,
		completed:    true,
	}
	if agg.provider == "" {
		agg.provider = "chatgpt"
	}
	agg.updateFromResponse(response)

	if output, ok := response["output"].([]any); ok {
		for _, item := range output {
			itemMap, _ := item.(map[string]any)
			itemType, _ := itemMap["type"].(string)
			switch itemType {
			case "message":
				content, _ := itemMap["content"].([]any)
				for _, raw := range content {
					part, _ := raw.(map[string]any)
					if part["type"] == "output_text" {
						if text, ok := part["text"].(string); ok {
							agg.text.WriteString(text)
						}
					}
				}
			case "function_call":
				agg.handleOutputItemDone(itemMap, func(stream.Event) {})
			}
		}
	}

	assistant := agg.buildAssistant()
	events := []stream.Event{{Type: stream.EventStart}}
	if text := extractText(assistant.ContentRaw); text != "" {
		events = append(events, stream.Event{
			Type:  stream.EventTextDelta,
			Delta: text,
		})
	}
	for _, call := range agg.toolCalls {
		events = append(events, stream.Event{
			Type:       stream.EventToolCall,
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Arguments:  call.Arguments,
		})
	}
	events = append(events, stream.Event{
		Type:   stream.EventDone,
		Reason: assistant.StopReason,
	})

	return &stream.StaticEventStream{
		Events:    events,
		ResultMsg: assistant,
	}, nil
}

func extractResponsesErrorMessage(response map[string]any) string {
	if len(response) == 0 {
		return "chatgpt backend returned response.failed"
	}
	if errObj, ok := response["error"].(map[string]any); ok {
		if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
		if code, ok := errObj["code"].(string); ok && strings.TrimSpace(code) != "" {
			return "chatgpt backend error: " + code
		}
	}
	return "chatgpt backend returned response.failed"
}

func intFromAny(raw any) int {
	switch v := raw.(type) {
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
	}
	return 0
}

func extractChatGPTAccountIDFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return ""
	}
	auth, ok := payload["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return ""
	}
	accountID, _ := auth["chatgpt_account_id"].(string)
	return strings.TrimSpace(accountID)
}
