package sdk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zahlmann/phi/agent"
	"github.com/zahlmann/phi/coding/session"
)

func TestRuntimeCreatesSessionAndProcessesPrompt(t *testing.T) {
	manager := session.NewInMemoryManager("s1")
	runtime := NewRuntime(func(sessionID string) (*AgentSession, error) {
		return CreateAgentSession(CreateSessionOptions{
			SystemPrompt:   "test",
			ThinkingLevel:  agent.ThinkingOff,
			SessionManager: manager,
		}), nil
	}, agent.QueueOptions{Workers: 1, BufferSize: 4, RetryDelay: time.Millisecond})

	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer runtime.Stop()

	if err := runtime.Enqueue(context.Background(), agent.InboundMessage{
		ID: "m1", SessionID: "s1", Text: "hello",
	}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	waitUntil(t, 500*time.Millisecond, func() bool {
		s, ok := runtime.GetSession("s1")
		return ok && len(s.State().Messages) == 1
	})
}

func TestRuntimeEnqueueHonorsContextCancellation(t *testing.T) {
	runtime := NewRuntime(func(string) (*AgentSession, error) {
		return nil, errors.New("should not be called")
	}, agent.QueueOptions{Workers: 1, BufferSize: 4, RetryDelay: time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runtime.Enqueue(ctx, agent.InboundMessage{SessionID: "s1", Text: "hello"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestRuntimeGetOrCreateSessionReusesExisting(t *testing.T) {
	factoryCalls := 0
	runtime := NewRuntime(func(sessionID string) (*AgentSession, error) {
		factoryCalls++
		return CreateAgentSession(CreateSessionOptions{
			SessionManager: session.NewInMemoryManager(sessionID),
		}), nil
	}, agent.QueueOptions{})

	first, err := runtime.getOrCreateSession("s1")
	if err != nil {
		t.Fatalf("first getOrCreate failed: %v", err)
	}
	second, err := runtime.getOrCreateSession("s1")
	if err != nil {
		t.Fatalf("second getOrCreate failed: %v", err)
	}

	if first != second {
		t.Fatal("expected same session instance")
	}
	if factoryCalls != 1 {
		t.Fatalf("expected factory to be called once, got %d", factoryCalls)
	}
}

func TestRuntimeFactoryValidation(t *testing.T) {
	runtime := NewRuntime(nil, agent.QueueOptions{})
	_, err := runtime.getOrCreateSession("s1")
	if err == nil || !strings.Contains(err.Error(), "session factory is required") {
		t.Fatalf("expected factory validation error, got %v", err)
	}
}

func TestRuntimeHandleInboundValidation(t *testing.T) {
	t.Run("requires session id", func(t *testing.T) {
		runtime := NewRuntime(func(string) (*AgentSession, error) {
			return nil, errors.New("should not be called")
		}, agent.QueueOptions{})
		err := runtime.handleInbound(context.Background(), agent.InboundMessage{Text: "hello"})
		if err == nil || !strings.Contains(err.Error(), "session id is required") {
			t.Fatalf("expected session id validation error, got %v", err)
		}
	})

	t.Run("requires non-empty text before creating session", func(t *testing.T) {
		factoryCalls := 0
		runtime := NewRuntime(func(string) (*AgentSession, error) {
			factoryCalls++
			return CreateAgentSession(CreateSessionOptions{
				SessionManager: session.NewInMemoryManager("s1"),
			}), nil
		}, agent.QueueOptions{})

		err := runtime.handleInbound(context.Background(), agent.InboundMessage{SessionID: "s1"})
		if err == nil || !strings.Contains(err.Error(), "inbound message text is empty") {
			t.Fatalf("expected inbound text validation error, got %v", err)
		}
		if factoryCalls != 0 {
			t.Fatalf("expected no session creation for empty inbound text, got %d factory calls", factoryCalls)
		}
	})

	t.Run("propagates factory errors", func(t *testing.T) {
		runtime := NewRuntime(func(string) (*AgentSession, error) {
			return nil, errors.New("factory failed")
		}, agent.QueueOptions{})
		err := runtime.handleInbound(context.Background(), agent.InboundMessage{
			SessionID: "s1",
			Text:      "hello",
		})
		if err == nil || !strings.Contains(err.Error(), "factory failed") {
			t.Fatalf("expected factory error, got %v", err)
		}
	})
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
