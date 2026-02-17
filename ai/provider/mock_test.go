package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/zahlmann/phi/ai/model"
)

func TestMockClientRequiresHandler(t *testing.T) {
	client := MockClient{}
	_, err := client.Stream(context.Background(), model.Model{}, model.Context{}, StreamOptions{})
	if err == nil || !strings.Contains(err.Error(), "mock handler is required") {
		t.Fatalf("expected handler validation error, got %v", err)
	}
}
