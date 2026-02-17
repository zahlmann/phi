package sdk

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/zahlmann/phi/agent"
)

type SessionFactory func(sessionID string) (*AgentSession, error)

type Runtime struct {
	queue    *agent.Queue
	factory  SessionFactory
	sessions map[string]*AgentSession
	mu       sync.RWMutex
}

func NewRuntime(factory SessionFactory, queueOptions agent.QueueOptions) *Runtime {
	if factory == nil {
		factory = func(string) (*AgentSession, error) {
			return nil, errors.New("session factory is required")
		}
	}
	rt := &Runtime{
		factory:  factory,
		sessions: map[string]*AgentSession{},
	}
	rt.queue = agent.NewQueue(rt.handleInbound, queueOptions)
	return rt
}

func (r *Runtime) Start(ctx context.Context) error {
	return r.queue.Start(ctx)
}

func (r *Runtime) Stop() {
	r.queue.Stop()
}

func (r *Runtime) Enqueue(ctx context.Context, message agent.InboundMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return r.queue.Enqueue(message)
	}
}

func (r *Runtime) GetSession(sessionID string) (*AgentSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.sessions[sessionID]
	return session, ok
}

func (r *Runtime) handleInbound(ctx context.Context, inbound agent.InboundMessage) error {
	if inbound.SessionID == "" {
		return errors.New("session id is required")
	}
	if strings.TrimSpace(inbound.Text) == "" {
		return errors.New("inbound message text is empty")
	}

	session, err := r.getOrCreateSession(inbound.SessionID)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return session.Prompt(inbound.Text, PromptOptions{})
	}
}

func (r *Runtime) getOrCreateSession(sessionID string) (*AgentSession, error) {
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	r.mu.RLock()
	existing, ok := r.sessions[sessionID]
	r.mu.RUnlock()
	if ok {
		return existing, nil
	}
	created, err := r.factory(sessionID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.sessions[sessionID]; ok {
		return existing, nil
	}
	r.sessions[sessionID] = created
	return created, nil
}
