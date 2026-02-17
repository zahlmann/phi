//go:build integration

package provider

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zahlmann/phi/ai/model"
)

func TestOpenAIClientLiveWithAPIKey(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY is not set")
	}

	modelID := os.Getenv("OPENAI_MODEL")
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	client := NewOpenAIClient()
	evStream, err := client.Stream(ctx, model.Model{
		Provider: "openai",
		ID:       modelID,
	}, model.Context{
		SystemPrompt: "Be concise.",
		Messages: []model.Message{
			{
				Role: model.RoleUser,
				ContentRaw: []any{
					model.TextContent{Type: model.ContentText, Text: "Reply with the single word: pong"},
				},
			},
		},
	}, StreamOptions{
		APIKey: apiKey,
	})
	if err != nil {
		t.Fatalf("stream setup failed: %v", err)
	}

	for {
		_, recvErr := evStream.Recv()
		if recvErr != nil {
			break
		}
	}

	out, err := evStream.Result()
	if err != nil {
		t.Fatalf("stream result failed: %v", err)
	}
	text := extractText(out.ContentRaw)
	if text == "" {
		t.Fatal("empty assistant text")
	}
}
