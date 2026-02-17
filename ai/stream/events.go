package stream

import "github.com/zahlmann/phi/ai/model"

type EventType string

const (
	EventStart         EventType = "start"
	EventTextDelta     EventType = "text_delta"
	EventThinkingDelta EventType = "thinking_delta"
	EventToolCall      EventType = "tool_call"
	EventDone          EventType = "done"
	EventError         EventType = "error"
)

type Event struct {
	Type       EventType               `json:"type"`
	Delta      string                  `json:"delta,omitempty"`
	ToolName   string                  `json:"toolName,omitempty"`
	ToolCallID string                  `json:"toolCallId,omitempty"`
	Arguments  map[string]any          `json:"arguments,omitempty"`
	Reason     model.StopReason        `json:"reason,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Partial    *model.AssistantMessage `json:"partial,omitempty"`
}

type EventStream interface {
	Recv() (Event, error)
	Result() (*model.AssistantMessage, error)
	Close() error
}
