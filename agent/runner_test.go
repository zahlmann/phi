package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	"github.com/zahlmann/phi/ai/stream"
)

func TestRunTurnValidation(t *testing.T) {
	t.Run("requires provider client", func(t *testing.T) {
		a := newTestAgent(nil)
		assistant, err := a.RunTurn(context.Background(), RunnerOptions{})
		if assistant != nil {
			t.Fatalf("expected nil assistant, got %#v", assistant)
		}
		if err == nil || !strings.Contains(err.Error(), "provider client is required") {
			t.Fatalf("expected provider client validation error, got %v", err)
		}
	})

	t.Run("requires model", func(t *testing.T) {
		a := New(State{})
		client := provider.MockClient{
			Handler: func(context.Context, model.Model, model.Context, provider.StreamOptions) (stream.EventStream, error) {
				t.Fatal("stream should not be called without model")
				return nil, nil
			},
		}
		assistant, err := a.RunTurn(context.Background(), RunnerOptions{Client: client})
		if assistant != nil {
			t.Fatalf("expected nil assistant, got %#v", assistant)
		}
		if err == nil || !strings.Contains(err.Error(), "model is required") {
			t.Fatalf("expected model validation error, got %v", err)
		}
	})
}

func TestRunTurnAppendsAssistantMessage(t *testing.T) {
	a := newTestAgent(nil)
	client := provider.MockClient{
		Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
			return textStream("hello", m), nil
		},
	}

	assistant, err := a.RunTurn(context.Background(), RunnerOptions{
		Client:    client,
		SessionID: "s1",
	})
	if err != nil {
		t.Fatalf("run turn failed: %v", err)
	}
	if assistant == nil {
		t.Fatal("assistant response is nil")
	}

	state := a.State()
	if len(state.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(state.Messages))
	}
	last, ok := state.Messages[1].(model.AssistantMessage)
	if !ok {
		t.Fatalf("expected assistant message type, got %T", state.Messages[1])
	}
	if got := extractTextFromContent(last.ContentRaw); got != "hello" {
		t.Fatalf("unexpected assistant text: %q", got)
	}
}

func TestRunTurnExecutesToolCalls(t *testing.T) {
	tool := &testTool{name: "write_file", resultText: "file written"}
	a := newTestAgent([]Tool{tool})
	client := provider.MockClient{
		Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
			if !conversationHasRole(conversation.Messages, model.RoleToolResult) {
				return toolCallStream("call_1", "write_file", map[string]any{
					"path":    "test.py",
					"content": "print('ok')",
				}, m), nil
			}
			return textStream("done", m), nil
		},
	}

	assistant, err := a.RunTurn(context.Background(), RunnerOptions{
		Client:    client,
		SessionID: "s2",
	})
	if err != nil {
		t.Fatalf("run turn failed: %v", err)
	}
	if assistant == nil {
		t.Fatal("assistant response is nil")
	}
	if tool.calls != 1 {
		t.Fatalf("expected tool to be called once, got %d", tool.calls)
	}

	state := a.State()
	// user + assistant(tool call) + tool result + assistant(final)
	if len(state.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(state.Messages))
	}
}

func TestRunTurnToolErrorsBecomeToolResultMessages(t *testing.T) {
	tests := []struct {
		name          string
		tools         []Tool
		toolName      string
		wantSubstring string
	}{
		{
			name:          "missing tool",
			tools:         nil,
			toolName:      "missing_tool",
			wantSubstring: "Tool not found: missing_tool",
		},
		{
			name: "tool execution error",
			tools: []Tool{
				&testTool{name: "broken_tool", executeErr: errors.New("boom")},
			},
			toolName:      "broken_tool",
			wantSubstring: "Tool execution error: boom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAgent(tc.tools)
			client := provider.MockClient{
				Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
					if !conversationHasRole(conversation.Messages, model.RoleToolResult) {
						return toolCallStream("call_1", tc.toolName, map[string]any{"path": "README.md"}, m), nil
					}
					return textStream("done", m), nil
				},
			}

			if _, err := a.RunTurn(context.Background(), RunnerOptions{Client: client, SessionID: "s3"}); err != nil {
				t.Fatalf("run turn failed: %v", err)
			}

			state := a.State()
			if len(state.Messages) != 4 {
				t.Fatalf("expected 4 messages, got %d", len(state.Messages))
			}
			toolResult, ok := state.Messages[2].(model.Message)
			if !ok {
				t.Fatalf("expected tool result message, got %T", state.Messages[2])
			}
			if toolResult.Role != model.RoleToolResult {
				t.Fatalf("expected tool result role, got %s", toolResult.Role)
			}
			text := extractTextFromContent(toolResult.ContentRaw)
			if !strings.Contains(text, tc.wantSubstring) {
				t.Fatalf("expected tool result to contain %q, got %q", tc.wantSubstring, text)
			}
		})
	}
}

func TestRunTurnReturnsErrorWhenToolRoundsExhausted(t *testing.T) {
	tool := &testTool{name: "loop_tool", resultText: "ok"}
	a := newTestAgent([]Tool{tool})
	client := provider.MockClient{
		Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
			return toolCallStream("call_1", "loop_tool", map[string]any{"n": 1}, m), nil
		},
	}

	assistant, err := a.RunTurn(context.Background(), RunnerOptions{
		Client:        client,
		SessionID:     "s4",
		MaxToolRounds: 2,
	})
	if err == nil || !strings.Contains(err.Error(), "max tool rounds reached") {
		t.Fatalf("expected max tool rounds error, got %v", err)
	}
	if assistant == nil {
		t.Fatal("expected last assistant response even when max rounds are exhausted")
	}
	if tool.calls != 2 {
		t.Fatalf("expected 2 tool calls, got %d", tool.calls)
	}

	state := a.State()
	// user + 2x (assistant tool call + tool result)
	if len(state.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(state.Messages))
	}
}

func TestExtractToolCalls(t *testing.T) {
	calls := extractToolCalls([]any{
		model.TextContent{Type: model.ContentText, Text: "ignore"},
		model.ToolCallContent{
			Type:      model.ContentToolCall,
			ID:        "call_typed",
			Name:      "typed_tool",
			Arguments: map[string]any{"a": 1},
		},
		map[string]any{
			"type":      string(model.ContentToolCall),
			"id":        "call_map",
			"name":      "map_tool",
			"arguments": map[string]any{"b": 2},
		},
		map[string]any{
			"type":      string(model.ContentToolCall),
			"id":        "call_no_args",
			"name":      "map_no_args",
			"arguments": "invalid",
		},
	})

	if len(calls) != 3 {
		t.Fatalf("expected 3 tool calls, got %d", len(calls))
	}
	if calls[0].Name != "typed_tool" || calls[0].Arguments["a"] != 1 {
		t.Fatalf("unexpected first call: %#v", calls[0])
	}
	if calls[1].Name != "map_tool" || calls[1].Arguments["b"] != 2 {
		t.Fatalf("unexpected second call: %#v", calls[1])
	}
	if calls[2].Name != "map_no_args" || len(calls[2].Arguments) != 0 {
		t.Fatalf("expected empty args for invalid argument payload, got %#v", calls[2])
	}
}

type testTool struct {
	name       string
	resultText string
	executeErr error
	calls      int
}

func (t *testTool) Name() string {
	return t.name
}

func (t *testTool) Description() string {
	return "test tool"
}

func (t *testTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}

func (t *testTool) Execute(toolCallID string, args map[string]any) (ToolResult, error) {
	t.calls++
	if t.executeErr != nil {
		return ToolResult{}, t.executeErr
	}
	return ToolResult{
		Content: []model.TextContent{
			{Type: model.ContentText, Text: t.resultText},
		},
	}, nil
}

func newTestAgent(tools []Tool) *Agent {
	return New(State{
		SystemPrompt: "You are helpful",
		Model: &model.Model{
			Provider: "mock",
			ID:       "test-model",
		},
		Thinking: ThinkingOff,
		Messages: []any{
			model.Message{
				Role:       model.RoleUser,
				ContentRaw: []any{model.TextContent{Type: model.ContentText, Text: "hi"}},
			},
		},
		Tools: tools,
	})
}

func textStream(text string, m model.Model) stream.EventStream {
	return &stream.MockStream{
		Events: []stream.Event{
			{Type: stream.EventStart},
			{Type: stream.EventTextDelta, Delta: text},
			{Type: stream.EventDone},
		},
		ResultValue: &model.AssistantMessage{
			Role:       model.RoleAssistant,
			ContentRaw: []any{model.TextContent{Type: model.ContentText, Text: text}},
			Provider:   m.Provider,
			Model:      m.ID,
			StopReason: model.StopReasonStop,
		},
	}
}

func toolCallStream(callID, name string, args map[string]any, m model.Model) stream.EventStream {
	return &stream.MockStream{
		Events: []stream.Event{
			{Type: stream.EventStart},
			{Type: stream.EventToolCall, ToolName: name, ToolCallID: callID, Arguments: args},
			{Type: stream.EventDone},
		},
		ResultValue: &model.AssistantMessage{
			Role: model.RoleAssistant,
			ContentRaw: []any{
				model.ToolCallContent{
					Type:      model.ContentToolCall,
					ID:        callID,
					Name:      name,
					Arguments: args,
				},
			},
			Provider:   m.Provider,
			Model:      m.ID,
			StopReason: model.StopReasonToolUse,
		},
	}
}

func conversationHasRole(messages []model.Message, role model.Role) bool {
	for _, message := range messages {
		if message.Role == role {
			return true
		}
	}
	return false
}

func extractTextFromContent(content []any) string {
	parts := make([]string, 0, len(content))
	for _, item := range content {
		if text, ok := item.(model.TextContent); ok && strings.TrimSpace(text.Text) != "" {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
