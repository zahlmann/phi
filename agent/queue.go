package agent

import (
	"context"
	"errors"
	"sync"
	"time"
)

type InboundMessage struct {
	ID         string            `json:"id"`
	SessionID  string            `json:"sessionId"`
	Text       string            `json:"text"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	ReceivedAt time.Time         `json:"receivedAt"`
}

type InboundHandler func(ctx context.Context, message InboundMessage) error

type QueueOptions struct {
	Workers    int
	BufferSize int
	MaxRetries int
	RetryDelay time.Duration
}

type Queue struct {
	handler InboundHandler
	opts    QueueOptions
	input   chan InboundMessage
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	mu      sync.Mutex
	running bool
}

func NewQueue(handler InboundHandler, options QueueOptions) *Queue {
	if options.Workers <= 0 {
		options.Workers = 1
	}
	if options.BufferSize <= 0 {
		options.BufferSize = 256
	}
	if options.MaxRetries < 0 {
		options.MaxRetries = 0
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = 200 * time.Millisecond
	}
	return &Queue{
		handler: handler,
		opts:    options,
		input:   make(chan InboundMessage, options.BufferSize),
	}
}

func (q *Queue) Start(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running {
		return errors.New("queue is already running")
	}
	if q.handler == nil {
		return errors.New("queue handler is required")
	}
	workerCtx, cancel := context.WithCancel(ctx)
	q.cancel = cancel
	q.running = true
	for i := 0; i < q.opts.Workers; i++ {
		q.wg.Add(1)
		go q.runWorker(workerCtx)
	}
	return nil
}

func (q *Queue) Stop() {
	q.mu.Lock()
	if !q.running {
		q.mu.Unlock()
		return
	}
	cancel := q.cancel
	q.running = false
	q.cancel = nil
	q.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	q.wg.Wait()
}

func (q *Queue) Enqueue(message InboundMessage) error {
	q.mu.Lock()
	running := q.running
	q.mu.Unlock()
	if !running {
		return errors.New("queue is not running")
	}
	select {
	case q.input <- message:
		return nil
	default:
		return errors.New("queue is full")
	}
}

func (q *Queue) runWorker(ctx context.Context) {
	defer q.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-q.input:
			q.handleWithRetry(ctx, msg)
		}
	}
}

func (q *Queue) handleWithRetry(ctx context.Context, message InboundMessage) {
	for attempt := 0; attempt <= q.opts.MaxRetries; attempt++ {
		if err := q.handler(ctx, message); err == nil {
			return
		}
		if attempt == q.opts.MaxRetries {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(q.opts.RetryDelay):
		}
	}
}
