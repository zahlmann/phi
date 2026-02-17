package stream

import (
	"errors"
	"testing"

	"github.com/zahlmann/phi/ai/model"
)

func TestMockStream(t *testing.T) {
	m := &MockStream{
		Events: []Event{
			{Type: EventStart},
			{Type: EventDone},
		},
		ResultValue: &model.AssistantMessage{
			Role: model.RoleAssistant,
		},
	}

	ev, err := m.Recv()
	if err != nil || ev.Type != EventStart {
		t.Fatalf("expected first event, got ev=%#v err=%v", ev, err)
	}
	_, _ = m.Recv()
	if _, err := m.Recv(); err == nil {
		t.Fatal("expected eof error")
	}
	if _, err := m.Result(); err != nil {
		t.Fatalf("result failed: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if _, err := m.Recv(); err == nil {
		t.Fatal("expected closed stream error")
	}
}

func TestMockStreamResultErrors(t *testing.T) {
	m := &MockStream{ResultErr: errors.New("boom")}
	if _, err := m.Result(); err == nil {
		t.Fatal("expected result error")
	}

	m = &MockStream{ResultValue: "not-assistant"}
	if _, err := m.Result(); err == nil {
		t.Fatal("expected bad result type error")
	}
}

func TestStaticEventStream(t *testing.T) {
	s := &StaticEventStream{
		Events: []Event{
			{Type: EventStart},
			{Type: EventDone},
		},
		ResultMsg: &model.AssistantMessage{Role: model.RoleAssistant},
	}

	ev, err := s.Recv()
	if err != nil || ev.Type != EventStart {
		t.Fatalf("expected first event, got ev=%#v err=%v", ev, err)
	}
	_, _ = s.Recv()
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected eof error")
	}
	if _, err := s.Result(); err != nil {
		t.Fatalf("result failed: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected closed stream error")
	}
}

func TestStaticEventStreamResultErrors(t *testing.T) {
	s := &StaticEventStream{ResultErr: errors.New("boom")}
	if _, err := s.Result(); err == nil {
		t.Fatal("expected result error")
	}

	s = &StaticEventStream{}
	if _, err := s.Result(); err == nil {
		t.Fatal("expected missing result error")
	}
}
