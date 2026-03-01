package agent

import "sync"

type Agent struct {
	mu       sync.RWMutex
	state    State
	handlers []func(Event)
	pendingQ []any
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
	a.Queue(message)
}

func (a *Agent) FollowUp(message any) {
	a.Queue(message)
}

func (a *Agent) Queue(message any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pendingQ = append(a.pendingQ, message)
}

func (a *Agent) PendingSteer() []any {
	return a.PendingQueue()
}

func (a *Agent) PendingFollowUp() []any {
	return a.PendingQueue()
}

func (a *Agent) PendingQueue() []any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]any, len(a.pendingQ))
	copy(out, a.pendingQ)
	return out
}

func (a *Agent) PendingCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.pendingQ)
}

func (a *Agent) DequeuePending() (any, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pendingQ) == 0 {
		return nil, false
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
