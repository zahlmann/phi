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
	manager := &recordingManager{id: "s1"}
	s := CreateAgentSession(CreateSessionOptions{
		SystemPrompt:   "help",
		ThinkingLevel:  agent.ThinkingOff,
		SessionManager: manager,
	})

	if err := s.Prompt("hello", PromptOptions{}); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}

	state := s.State()
	if len(state.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(state.Messages))
	}
	if len(manager.appended) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(manager.appended))
	}
}

func TestSessionPromptIncludesImages(t *testing.T) {
	manager := &recordingManager{id: "s1"}
	s := CreateAgentSession(CreateSessionOptions{
		SessionManager: manager,
	})

	image := model.ImageContent{
		Type:     model.ContentImage,
		MIMEType: "image/png",
		Data:     "abc",
	}
	if err := s.Prompt("hello", PromptOptions{Images: []model.ImageContent{image}}); err != nil {
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
	manager := &recordingManager{id: "s1"}
	client := provider.MockClient{
		Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
			return textStream("ok", m), nil
		},
	}

	s := CreateAgentSession(CreateSessionOptions{
		SystemPrompt:   "help",
		Model:          &model.Model{Provider: "mock", ID: "m1"},
		ThinkingLevel:  agent.ThinkingOff,
		SessionManager: manager,
		ProviderClient: client,
	})

	if err := s.Prompt("hello", PromptOptions{}); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	state := s.State()
	if len(state.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(state.Messages))
	}
	if len(manager.appended) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(manager.appended))
	}
	if _, ok := manager.appended[1].(model.AssistantMessage); !ok {
		t.Fatalf("expected assistant message to be persisted, got %T", manager.appended[1])
	}
}

func TestSessionPromptExecutesTools(t *testing.T) {
	manager := &recordingManager{id: "s2"}
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
		ThinkingLevel:  agent.ThinkingOff,
		Tools:          []agent.Tool{tool},
		SessionManager: manager,
		ProviderClient: client,
	})
	if err := s.Prompt("hello", PromptOptions{}); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if tool.calls != 1 {
		t.Fatalf("expected tool call once, got %d", tool.calls)
	}
	state := s.State()
	if len(state.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(state.Messages))
	}
	if len(manager.appended) != 4 {
		t.Fatalf("expected all 4 messages to be persisted, got %d", len(manager.appended))
	}
}

func TestSessionPromptErrorPaths(t *testing.T) {
	t.Run("manager append error", func(t *testing.T) {
		manager := &recordingManager{id: "s1", appendErr: errors.New("persist failed")}
		s := CreateAgentSession(CreateSessionOptions{
			SessionManager: manager,
		})
		err := s.Prompt("hello", PromptOptions{})
		if err == nil || !strings.Contains(err.Error(), "persist failed") {
			t.Fatalf("expected manager error, got %v", err)
		}
	})

	t.Run("provider error", func(t *testing.T) {
		manager := &recordingManager{id: "s1"}
		client := provider.MockClient{
			Handler: func(context.Context, model.Model, model.Context, provider.StreamOptions) (stream.EventStream, error) {
				return nil, errors.New("provider failed")
			},
		}
		s := CreateAgentSession(CreateSessionOptions{
			Model:          &model.Model{Provider: "mock", ID: "m1"},
			SessionManager: manager,
			ProviderClient: client,
		})
		err := s.Prompt("hello", PromptOptions{})
		if err == nil || !strings.Contains(err.Error(), "provider failed") {
			t.Fatalf("expected provider error, got %v", err)
		}
		if len(manager.appended) != 1 {
			t.Fatalf("expected user message to be persisted before provider failure, got %d", len(manager.appended))
		}
	})
}

func TestSessionSteerAndFollowUpQueue(t *testing.T) {
	s := CreateAgentSession(CreateSessionOptions{
		SessionManager: &recordingManager{id: "s1"},
	})
	s.Steer("be concise")
	s.FollowUp("and include tests")

	steer := s.agent.PendingSteer()
	if len(steer) != 1 {
		t.Fatalf("expected 1 steer message, got %d", len(steer))
	}
	follow := s.agent.PendingFollowUp()
	if len(follow) != 1 {
		t.Fatalf("expected 1 follow-up message, got %d", len(follow))
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

type recordingManager struct {
	id        string
	appended  []any
	appendErr error
}

func (m *recordingManager) SessionID() string {
	return m.id
}

func (m *recordingManager) SessionFile() string {
	return ""
}

func (m *recordingManager) AppendMessage(message any) (string, error) {
	if m.appendErr != nil {
		return "", m.appendErr
	}
	m.appended = append(m.appended, message)
	return "entry", nil
}

func (m *recordingManager) AppendModelChange(provider, modelID string) (string, error) {
	return "model", nil
}

func (m *recordingManager) AppendThinkingLevelChange(level string) (string, error) {
	return "thinking", nil
}

func (m *recordingManager) BuildContext() ([]any, string, string, string) {
	return append([]any{}, m.appended...), "off", "", ""
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
