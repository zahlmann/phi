package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	"github.com/zahlmann/phi/ai/stream"
)

func TestSessionPromptWithoutProviderAppendsUserMessage(t *testing.T) {
	s := CreateAgentSession(CreateSessionOptions{
		SystemPrompt:  "help",
		ThinkingLevel: agent.ThinkingNone,
	})

	if err := s.Prompt("hello", nil); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}

	state := s.State()
	if len(state.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(state.Messages))
	}
}

func TestSessionPromptIncludesImages(t *testing.T) {
	s := CreateAgentSession(CreateSessionOptions{
		SystemPrompt: "help",
	})

	image := model.ImageContent{
		Type:     model.ContentImage,
		MIMEType: "image/png",
		Data:     "abc",
	}
	if err := s.Prompt("hello", []model.ImageContent{image}); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}

	msg, ok := s.State().Messages[0].(model.Message)
	if !ok {
		t.Fatalf("expected user message, got %T", s.State().Messages[0])
	}
	if len(msg.ContentRaw) != 2 {
		t.Fatalf("expected text + image in content, got %d items", len(msg.ContentRaw))
	}
}

func TestSessionPromptRunsProviderTurnAndPersistsAssistantMessages(t *testing.T) {
	client := provider.MockClient{
		Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
			return textStream("ok", m), nil
		},
	}

	s := CreateAgentSession(CreateSessionOptions{
		SystemPrompt:   "help",
		Model:          &model.Model{Provider: "mock", ID: "m1"},
		ThinkingLevel:  agent.ThinkingNone,
		ProviderClient: client,
	})

	if err := s.Prompt("hello", nil); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	state := s.State()
	if len(state.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(state.Messages))
	}
	if _, ok := state.Messages[1].(model.AssistantMessage); !ok {
		t.Fatalf("expected assistant message, got %T", state.Messages[1])
	}
}

func TestSessionPromptExecutesTools(t *testing.T) {
	tool := &testWriteTool{}
	client := provider.MockClient{
		Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
			if !conversationHasRole(conversation.Messages, model.RoleToolResult) {
				return toolCallStream("call_1", "write_file", map[string]any{
					"path":    "a.py",
					"content": "print('ok')",
				}, m), nil
			}
			return textStream("done", m), nil
		},
	}

	s := CreateAgentSession(CreateSessionOptions{
		SystemPrompt:   "help",
		Model:          &model.Model{Provider: "mock", ID: "m1"},
		ThinkingLevel:  agent.ThinkingNone,
		Tools:          []agent.Tool{tool},
		ProviderClient: client,
	})
	if err := s.Prompt("hello", nil); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if tool.calls != 1 {
		t.Fatalf("expected tool call once, got %d", tool.calls)
	}
	state := s.State()
	if len(state.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(state.Messages))
	}
}

func TestSessionPromptErrorPaths(t *testing.T) {
	t.Run("provider error", func(t *testing.T) {
		client := provider.MockClient{
			Handler: func(context.Context, model.Model, model.Context, provider.StreamOptions) (stream.EventStream, error) {
				return nil, errors.New("provider failed")
			},
		}
		s := CreateAgentSession(CreateSessionOptions{
			Model:          &model.Model{Provider: "mock", ID: "m1"},
			ProviderClient: client,
		})
		err := s.Prompt("hello", nil)
		if err == nil || !strings.Contains(err.Error(), "provider failed") {
			t.Fatalf("expected provider error, got %v", err)
		}
		if len(s.State().Messages) != 1 {
			t.Fatalf("expected only the user message on provider failure, got %d", len(s.State().Messages))
		}
	})
}

func TestSessionQueueMessageAppendsToPendingQueue(t *testing.T) {
	s := CreateAgentSession(CreateSessionOptions{})
	s.QueueMessage("be concise", nil)
	s.QueueMessage("and include tests", nil)

	if got := s.PendingCount(); got != 2 {
		t.Fatalf("expected 2 queued messages, got %d", got)
	}
}

func TestAppendAssistantMessage(t *testing.T) {
	s := CreateAgentSession(CreateSessionOptions{})
	assistant := model.AssistantMessage{
		Role:       model.RoleAssistant,
		ContentRaw: []any{model.TextContent{Type: model.ContentText, Text: "ok"}},
	}
	if err := s.AppendAssistantMessage(assistant); err != nil {
		t.Fatalf("append assistant failed: %v", err)
	}
	state := s.State()
	if len(state.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(state.Messages))
	}
	if _, ok := state.Messages[0].(model.AssistantMessage); !ok {
		t.Fatalf("expected assistant message, got %T", state.Messages[0])
	}
}

type testWriteTool struct {
	calls int
}

func (t *testWriteTool) Name() string {
	return "write_file"
}

func (t *testWriteTool) Description() string {
	return "test write tool"
}

func (t *testWriteTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}

func (t *testWriteTool) Execute(toolCallID string, args map[string]any) (agent.ToolResult, error) {
	t.calls++
	return agent.ToolResult{
		Content: []model.TextContent{
			{Type: model.ContentText, Text: "ok"},
		},
	}, nil
}

func textStream(text string, m model.Model) stream.EventStream {
	return &stream.StaticEventStream{
		Events: []stream.Event{
			{Type: stream.EventStart},
			{Type: stream.EventTextDelta, Delta: text},
			{Type: stream.EventDone},
		},
		ResultMsg: &model.AssistantMessage{
			Role:       model.RoleAssistant,
			ContentRaw: []any{model.TextContent{Type: model.ContentText, Text: text}},
			Provider:   m.Provider,
			Model:      m.ID,
			StopReason: model.StopReasonStop,
		},
	}
}

func toolCallStream(callID, name string, args map[string]any, m model.Model) stream.EventStream {
	return &stream.StaticEventStream{
		Events: []stream.Event{
			{Type: stream.EventStart},
			{Type: stream.EventToolCall, ToolName: name, ToolCallID: callID, Arguments: args},
			{Type: stream.EventDone},
		},
		ResultMsg: &model.AssistantMessage{
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
