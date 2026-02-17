package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewQueueAppliesDefaults(t *testing.T) {
	q := NewQueue(func(context.Context, InboundMessage) error { return nil }, QueueOptions{})
	if q.opts.Workers != 1 {
		t.Fatalf("expected default workers=1, got %d", q.opts.Workers)
	}
	if q.opts.BufferSize != 256 {
		t.Fatalf("expected default buffer size=256, got %d", q.opts.BufferSize)
	}
	if q.opts.MaxRetries != 0 {
		t.Fatalf("expected default max retries=0, got %d", q.opts.MaxRetries)
	}
	if q.opts.RetryDelay != 200*time.Millisecond {
		t.Fatalf("expected default retry delay=200ms, got %s", q.opts.RetryDelay)
	}
}

func TestQueueStartValidation(t *testing.T) {
	t.Run("requires handler", func(t *testing.T) {
		q := NewQueue(nil, QueueOptions{})
		err := q.Start(context.Background())
		if err == nil || !strings.Contains(err.Error(), "handler is required") {
			t.Fatalf("expected handler validation error, got %v", err)
		}
	})

	t.Run("rejects second start", func(t *testing.T) {
		q := NewQueue(func(context.Context, InboundMessage) error { return nil }, QueueOptions{})
		if err := q.Start(context.Background()); err != nil {
			t.Fatalf("first start failed: %v", err)
		}
		defer q.Stop()

		err := q.Start(context.Background())
		if err == nil || !strings.Contains(err.Error(), "already running") {
			t.Fatalf("expected already-running error, got %v", err)
		}
	})
}

func TestQueueEnqueueValidation(t *testing.T) {
	t.Run("requires running queue", func(t *testing.T) {
		q := NewQueue(func(context.Context, InboundMessage) error { return nil }, QueueOptions{})
		err := q.Enqueue(InboundMessage{ID: "1"})
		if err == nil || !strings.Contains(err.Error(), "not running") {
			t.Fatalf("expected not-running error, got %v", err)
		}
	})

	t.Run("returns queue full", func(t *testing.T) {
		started := make(chan struct{}, 1)
		release := make(chan struct{})
		q := NewQueue(func(context.Context, InboundMessage) error {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			return nil
		}, QueueOptions{Workers: 1, BufferSize: 1, RetryDelay: time.Millisecond})

		if err := q.Start(context.Background()); err != nil {
			t.Fatalf("start failed: %v", err)
		}
		defer q.Stop()

		if err := q.Enqueue(InboundMessage{ID: "1"}); err != nil {
			t.Fatalf("first enqueue failed: %v", err)
		}
		waitUntil(t, 500*time.Millisecond, func() bool {
			select {
			case <-started:
				return true
			default:
				return false
			}
		})

		if err := q.Enqueue(InboundMessage{ID: "2"}); err != nil {
			t.Fatalf("second enqueue failed: %v", err)
		}
		err := q.Enqueue(InboundMessage{ID: "3"})
		if err == nil || !strings.Contains(err.Error(), "queue is full") {
			t.Fatalf("expected queue full error, got %v", err)
		}
		close(release)
	})
}

func TestQueueProcessesMessages(t *testing.T) {
	var seen int32
	q := NewQueue(func(ctx context.Context, message InboundMessage) error {
		atomic.AddInt32(&seen, 1)
		return nil
	}, QueueOptions{Workers: 1, BufferSize: 4, RetryDelay: time.Millisecond})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer q.Stop()

	if err := q.Enqueue(InboundMessage{ID: "1", SessionID: "s1", Text: "hello"}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	waitUntil(t, 500*time.Millisecond, func() bool {
		return atomic.LoadInt32(&seen) == 1
	})
}

func TestQueueRetriesFailures(t *testing.T) {
	var attempts int32
	q := NewQueue(func(ctx context.Context, message InboundMessage) error {
		current := atomic.AddInt32(&attempts, 1)
		if current < 3 {
			return errors.New("retry")
		}
		return nil
	}, QueueOptions{Workers: 1, BufferSize: 2, MaxRetries: 3, RetryDelay: time.Millisecond})

	if err := q.Start(context.Background()); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer q.Stop()

	if err := q.Enqueue(InboundMessage{ID: "1", SessionID: "s1", Text: "retry"}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	waitUntil(t, 500*time.Millisecond, func() bool {
		return atomic.LoadInt32(&attempts) >= 3
	})
}

func TestQueueStopsRetryOnCancel(t *testing.T) {
	var attempts int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q := NewQueue(func(context.Context, InboundMessage) error {
		if atomic.AddInt32(&attempts, 1) == 1 {
			cancel()
		}
		return errors.New("fail")
	}, QueueOptions{Workers: 1, BufferSize: 1, MaxRetries: 5, RetryDelay: 20 * time.Millisecond})

	if err := q.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer q.Stop()

	if err := q.Enqueue(InboundMessage{ID: "1", SessionID: "s1", Text: "retry"}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	waitUntil(t, 500*time.Millisecond, func() bool {
		return atomic.LoadInt32(&attempts) >= 1
	})
	time.Sleep(30 * time.Millisecond)
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected retry loop to stop at one attempt after cancel, got %d", got)
	}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
