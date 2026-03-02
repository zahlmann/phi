package stream

import (
	"errors"
	"io"

	"github.com/zahlmann/phi/ai/model"
)

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

type StaticEventStream struct {
	Events    []Event
	ResultMsg *model.AssistantMessage
	ResultErr error
	index     int
	closed    bool
}

func (s *StaticEventStream) Recv() (Event, error) {
	if s.closed {
		return Event{}, errors.New("stream closed")
	}
	if s.index >= len(s.Events) {
		return Event{}, io.EOF
	}
	ev := s.Events[s.index]
	s.index++
	return ev, nil
}

func (s *StaticEventStream) Result() (*model.AssistantMessage, error) {
	if s.ResultErr != nil {
		return nil, s.ResultErr
	}
	if s.ResultMsg == nil {
		return nil, errors.New("no result")
	}
	return s.ResultMsg, nil
}

func (s *StaticEventStream) Close() error {
	s.closed = true
	return nil
}
