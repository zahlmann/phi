package provider

import (
	"context"

	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/stream"
)

type AuthMode string

const (
	AuthModeOpenAIAPIKey AuthMode = "openai_api_key"
	AuthModeChatGPT      AuthMode = "chatgpt"
)

type StreamOptions struct {
	AuthMode    AuthMode
	APIKey      string
	AccessToken string
	AccountID   string
	SessionID   string
	BaseURL     string
	Headers     map[string]string
	Temperature *float64
	MaxTokens   int
}

type Client interface {
	Stream(ctx context.Context, model model.Model, conversation model.Context, options StreamOptions) (stream.EventStream, error)
}
