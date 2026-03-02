package phi

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zahlmann/phi/ai/model"
	"github.com/zahlmann/phi/ai/provider"
	"github.com/zahlmann/phi/ai/stream"
)

func TestRuntimeStartSessionAutoIDAndFinalEvent(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{
		ProviderClient: provider.MockClient{
			Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
				return staticTextStream("ok", m), nil
			},
		},
	})
	defer rt.Close()

	finalEvents := make(chan Event, 1)
	unsubscribe := rt.Subscribe(func(event Event) {
		if event.Type == EventFinalMessage {
			finalEvents <- event
		}
	})
	defer unsubscribe()

	started, err := rt.StartSession(context.Background(), StartSessionRequest{Prompt: "hello"})
	if err != nil {
		t.Fatalf("start session failed: %v", err)
	}
	if started.SessionID == "" {
		t.Fatal("expected auto-generated session id")
	}

	select {
	case ev := <-finalEvents:
		if ev.SessionID != started.SessionID {
			t.Fatalf("expected session id %q, got %q", started.SessionID, ev.SessionID)
		}
		if ev.AssistantMessage == nil {
			t.Fatal("expected final assistant message")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for final event")
	}
}

func TestRuntimeQueueMessageInjectedAfterToolBoundary(t *testing.T) {
	var round int32
	var sawInjectedAtRound2 atomic.Bool

	rt := NewRuntime(RuntimeOptions{
		ProviderClient: provider.MockClient{
			Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
				current := atomic.AddInt32(&round, 1)
				switch current {
				case 1:
					return staticToolCallStream("call_1", "bash", map[string]any{
						"command": "sleep 0.2; echo first",
					}, m), nil
				case 2:
					if conversationHasUserText(conversation.Messages, "second message") {
						sawInjectedAtRound2.Store(true)
					}
					return staticTextStream("done", m), nil
				default:
					return staticTextStream("unexpected extra round", m), nil
				}
			},
		},
	})
	defer rt.Close()

	toolStarted := make(chan struct{}, 1)
	final := make(chan Event, 1)
	unsubscribe := rt.Subscribe(func(event Event) {
		switch event.Type {
		case EventToolCallStarted:
			select {
			case toolStarted <- struct{}{}:
			default:
			}
		case EventFinalMessage:
			select {
			case final <- event:
			default:
			}
		}
	})
	defer unsubscribe()

	started, err := rt.StartSession(context.Background(), StartSessionRequest{Prompt: "first message"})
	if err != nil {
		t.Fatalf("start session failed: %v", err)
	}

	select {
	case <-toolStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for tool start")
	}

	if err := rt.QueueMessage(context.Background(), QueueMessageRequest{
		SessionID: started.SessionID,
		Prompt:    "second message",
	}); err != nil {
		t.Fatalf("queue message failed: %v", err)
	}

	select {
	case <-final:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for final event")
	}

	if !sawInjectedAtRound2.Load() {
		t.Fatal("expected queued message to be injected before round 2 provider call")
	}
	if got := atomic.LoadInt32(&round); got != 2 {
		t.Fatalf("expected exactly 2 provider rounds, got %d", got)
	}
}

func TestRuntimeUsesXHighReasoningEffort(t *testing.T) {
	var observed string
	rt := NewRuntime(RuntimeOptions{
		ProviderClient: provider.MockClient{
			Handler: func(ctx context.Context, m model.Model, conversation model.Context, options provider.StreamOptions) (stream.EventStream, error) {
				observed = options.ReasoningEffort
				return staticTextStream("ok", m), nil
			},
		},
	})
	defer rt.Close()

	if _, err := rt.StartSession(context.Background(), StartSessionRequest{Prompt: "hello"}); err != nil {
		t.Fatalf("start session failed: %v", err)
	}

	waitUntil(t, 3*time.Second, func() bool {
		return observed != ""
	})

	if observed != "xhigh" {
		t.Fatalf("expected reasoning effort xhigh, got %q", observed)
	}
}

func staticTextStream(text string, m model.Model) stream.EventStream {
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

func staticToolCallStream(callID, name string, args map[string]any, m model.Model) stream.EventStream {
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

func conversationHasUserText(messages []model.Message, text string) bool {
	for _, message := range messages {
		if message.Role != model.RoleUser {
			continue
		}
		for _, item := range message.ContentRaw {
			if tc, ok := item.(model.TextContent); ok && tc.Text == text {
				return true
			}
		}
	}
	return false
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
