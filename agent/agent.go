package agent

import (
	"sync"

	"github.com/zahlmann/phi/ai/model"
)

type ThinkingLevel string

const (
	ThinkingNone    ThinkingLevel = "none"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
)

type EventType string

const (
	EventTurnStart          EventType = "turn_start"
	EventTurnEnd            EventType = "turn_end"
	EventMessageStart       EventType = "message_start"
	EventMessageUpdate      EventType = "message_update"
	EventMessageEnd         EventType = "message_end"
	EventToolExecutionStart EventType = "tool_execution_start"
	EventToolExecutionEnd   EventType = "tool_execution_end"
)

type Event struct {
	Type       EventType `json:"type"`
	Message    any       `json:"message,omitempty"`
	ToolName   string    `json:"toolName,omitempty"`
	ToolCallID string    `json:"toolCallId,omitempty"`
	IsError    bool      `json:"isError,omitempty"`
}

type ToolResult struct {
	Content []model.TextContent `json:"content"`
	Details map[string]any      `json:"details,omitempty"`
}

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(toolCallID string, args map[string]any) (ToolResult, error)
}

type State struct {
	SystemPrompt string        `json:"systemPrompt"`
	Model        *model.Model  `json:"model,omitempty"`
	Thinking     ThinkingLevel `json:"thinkingLevel"`
	Messages     []any         `json:"messages"`
	IsStreaming  bool          `json:"isStreaming"`
	Tools        []Tool        `json:"-"`
}

type Agent struct {
	mu       sync.RWMutex
	state    State
	handlers []func(Event)
	pendingQ []model.Message
}

func New(initial State) *Agent {
	return &Agent{state: initial}
}

func (a *Agent) State() State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

func (a *Agent) Subscribe(handler func(Event)) (unsubscribe func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers = append(a.handlers, handler)
	idx := len(a.handlers) - 1
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if idx >= 0 && idx < len(a.handlers) {
			a.handlers[idx] = nil
		}
	}
}

func (a *Agent) emit(event Event) {
	a.mu.RLock()
	handlers := append([]func(Event){}, a.handlers...)
	a.mu.RUnlock()
	for _, h := range handlers {
		if h != nil {
			h(event)
		}
	}
}

func (a *Agent) Prompt(message any) {
	a.mu.Lock()
	a.state.Messages = append(a.state.Messages, message)
	a.mu.Unlock()
	a.emit(Event{Type: EventMessageStart, Message: message})
	a.emit(Event{Type: EventMessageEnd, Message: message})
}

func (a *Agent) Queue(message model.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pendingQ = append(a.pendingQ, message)
}

func (a *Agent) PendingCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.pendingQ)
}

func (a *Agent) DequeuePending() (model.Message, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pendingQ) == 0 {
		return model.Message{}, false
	}
	next := a.pendingQ[0]
	a.pendingQ = a.pendingQ[1:]
	return next, true
}

func (a *Agent) AddMessage(message any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Messages = append(a.state.Messages, message)
}
