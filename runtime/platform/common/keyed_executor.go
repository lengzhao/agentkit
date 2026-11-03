package common

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrExecutorFull    = errors.New("platform ingress queue is full")
	ErrExecutorStopped = errors.New("platform ingress executor stopped")
)

// KeyedExecutor admits work without blocking SDK callbacks. It preserves FIFO
// order per key, bounds active work and waiting tasks, and has no idle workers.
// Work must honor ctx; Stop cancels active work and discards queued closures.
type KeyedExecutor struct {
	mu                                     sync.Mutex
	ctx                                    context.Context
	cancel                                 context.CancelFunc
	concurrency, capacity, running, queued int
	closed                                 bool
	queues                                 map[string][]func(context.Context)
	busy                                   map[string]bool
	ready                                  []string
	done                                   chan struct{}
}

func NewKeyedExecutor(concurrency, capacity int) *KeyedExecutor {
	if concurrency <= 0 {
		concurrency = 1
	}
	if capacity <= 0 {
		capacity = 64
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &KeyedExecutor{ctx: ctx, cancel: cancel, concurrency: concurrency, capacity: capacity, queues: map[string][]func(context.Context){}, busy: map[string]bool{}, done: make(chan struct{})}
}

// Bind attaches host cancellation without inheriting the SDK request lifetime.
func (e *KeyedExecutor) Bind(ctx context.Context) {
	if ctx.Err() != nil {
		e.Cancel()
		return
	}
	context.AfterFunc(ctx, e.Cancel)
}
func (e *KeyedExecutor) Submit(key string, work func(context.Context)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrExecutorStopped
	}
	if e.queued >= e.capacity {
		return ErrExecutorFull
	}
	if len(e.queues[key]) == 0 && !e.busy[key] {
		e.ready = append(e.ready, key)
	}
	e.queues[key] = append(e.queues[key], work)
	e.queued++
	e.launch()
	return nil
}

// launch is called under mu. Queued keys rejoin the tail after each task, so a
// busy conversation cannot monopolize all progress on other conversations.
func (e *KeyedExecutor) launch() {
	for e.running < e.concurrency && len(e.ready) > 0 {
		key := e.ready[0]
		e.ready = e.ready[1:]
		queue := e.queues[key]
		work := queue[0]
		queue[0] = nil
		e.queues[key] = queue[1:]
		e.queued--
		e.running++
		e.busy[key] = true
		go e.run(key, work)
	}
}
func (e *KeyedExecutor) run(key string, work func(context.Context)) {
	defer func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.running--
		delete(e.busy, key)
		if e.closed {
			if e.running == 0 {
				close(e.done)
			}
			return
		}
		if len(e.queues[key]) > 0 {
			e.ready = append(e.ready, key)
		} else {
			delete(e.queues, key)
		}
		e.launch()
	}()
	if e.ctx.Err() == nil && work != nil {
		work(e.ctx)
	}
}
func (e *KeyedExecutor) Cancel() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	e.closed = true
	e.cancel()
	e.queues = nil
	e.ready = nil
	e.queued = 0
	if e.running == 0 {
		close(e.done)
	}
}
func (e *KeyedExecutor) Stop(ctx context.Context) error {
	e.Cancel()
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
