package provider

import (
	"context"
	"errors"

	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/stream"
)

type MockClient struct {
	Handler func(ctx context.Context, m model.Model, conversation model.Context, options StreamOptions) (stream.EventStream, error)
}

func (m MockClient) Stream(ctx context.Context, mod model.Model, conversation model.Context, options StreamOptions) (stream.EventStream, error) {
	if m.Handler == nil {
		return nil, errors.New("mock handler is required")
	}
	return m.Handler(ctx, mod, conversation, options)
}
