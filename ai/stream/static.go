package stream

import (
	"errors"
	"io"

	"github.com/zahlmann/phi/ai/model"
)

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
