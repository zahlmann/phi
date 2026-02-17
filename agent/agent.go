package agent

import "sync"

type Agent struct {
	mu       sync.RWMutex
	state    State
	handlers []func(Event)
	steerQ   []any
	followQ  []any
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

func (a *Agent) Steer(message any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.steerQ = append(a.steerQ, message)
}

func (a *Agent) FollowUp(message any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.followQ = append(a.followQ, message)
}

func (a *Agent) PendingSteer() []any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]any, len(a.steerQ))
	copy(out, a.steerQ)
	return out
}

func (a *Agent) PendingFollowUp() []any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]any, len(a.followQ))
	copy(out, a.followQ)
	return out
}
