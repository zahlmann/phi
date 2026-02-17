package stream

import (
	"errors"
	"io"

	"github.com/zahlmann/phi/ai/model"
)

type MockStream struct {
	Events      []Event
	ResultValue any
	ResultErr   error
	index       int
	closed      bool
}

func (m *MockStream) Recv() (Event, error) {
	if m.closed {
		return Event{}, errors.New("stream closed")
	}
	if m.index >= len(m.Events) {
		return Event{}, io.EOF
	}
	out := m.Events[m.index]
	m.index++
	return out, nil
}

func (m *MockStream) Result() (*model.AssistantMessage, error) {
	if m.ResultErr != nil {
		return nil, m.ResultErr
	}
	if m.ResultValue == nil {
		return nil, errors.New("no result")
	}
	msg, ok := m.ResultValue.(*model.AssistantMessage)
	if !ok {
		return nil, errors.New("result is not assistant message")
	}
	return msg, nil
}

func (m *MockStream) Close() error {
	m.closed = true
	return nil
}
