package openai

import (
	"context"
	"time"
)

type Credentials struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	AccountID    string    `json:"accountId,omitempty"`
}

type TokenStore interface {
	Load(ctx context.Context) (*Credentials, error)
	Save(ctx context.Context, credentials *Credentials) error
	Clear(ctx context.Context) error
}

type DeviceCode struct {
	DeviceCode      string `json:"deviceCode"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

type Client interface {
	StartDeviceFlow(ctx context.Context) (*DeviceCode, error)
	PollDeviceFlow(ctx context.Context, code *DeviceCode) (*Credentials, error)
	Refresh(ctx context.Context, refreshToken string) (*Credentials, error)
}

type Manager struct {
	Client Client
	Store  TokenStore
}

func (m *Manager) LoadOrRefresh(ctx context.Context) (*Credentials, error) {
	current, err := m.Store.Load(ctx)
	if err != nil || current == nil {
		return nil, err
	}
	if time.Now().Before(current.ExpiresAt.Add(-30 * time.Second)) {
		return current, nil
	}
	next, err := m.Client.Refresh(ctx, current.RefreshToken)
	if err != nil {
		return nil, err
	}
	if err := m.Store.Save(ctx, next); err != nil {
		return nil, err
	}
	return next, nil
}
