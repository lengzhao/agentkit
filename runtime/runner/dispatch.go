package runner

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// scheduler runs turns from distinct sessions in parallel while keeping each
// session's own messages in arrival order.
//
// Per-session ordering is enforced here with one worker per session drained in
// FIFO order. Session queue bookkeeping is owned by a single goroutine (regCh).
//
// Concurrency is capped by a slot semaphore: a slot is acquired when a worker
// starts dispatching a request and released when that dispatch finishes. The
// platform receive path does not take slots, so inbound can still be read while
// a turn waits on human input (permission pending).
//
// Session queues may hold more requests than maxConcurrent; only in-flight
// dispatches are capped.
type scheduler struct {
	dispatch func(context.Context, agentkit.LoopRequest) error
	onError  func(context.Context, agentkit.LoopRequest, error)
	slots    chan struct{}

	regCh  chan func(map[agentkit.SessionID]*sessionQueue)
	queues map[agentkit.SessionID]*sessionQueue
	wg     sync.WaitGroup
}

// sessionQueue is one session's FIFO backlog plus whether a worker owns it.
type sessionQueue struct {
	pending []agentkit.LoopRequest
	running bool
}

func newScheduler(
	maxConcurrent int,
	dispatch func(context.Context, agentkit.LoopRequest) error,
	onError func(context.Context, agentkit.LoopRequest, error),
) *scheduler {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	s := &scheduler{
		dispatch: dispatch,
		onError:  onError,
		slots:    make(chan struct{}, maxConcurrent),
		regCh:    make(chan func(map[agentkit.SessionID]*sessionQueue)),
		queues:   make(map[agentkit.SessionID]*sessionQueue),
	}
	go s.registryLoop()
	return s
}

func (s *scheduler) registryLoop() {
	for fn := range s.regCh {
		fn(s.queues)
	}
}

// stop retires the registry goroutine. Call after wait returns so no further
// submits race the close. Safe to call once.
func (s *scheduler) stop() {
	close(s.regCh)
}

func (s *scheduler) withQueues(fn func(map[agentkit.SessionID]*sessionQueue)) {
	done := make(chan struct{})
	s.regCh <- func(queues map[agentkit.SessionID]*sessionQueue) {
		fn(queues)
		close(done)
	}
	<-done
}

// acquire takes one concurrency slot before running a turn.
func (s *scheduler) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.slots <- struct{}{}:
		return nil
	}
}

// release returns a slot after dispatch finishes.
func (s *scheduler) release() {
	select {
	case <-s.slots:
	default:
	}
}

// submit queues a new turn request, starting a worker for the session when one
// is not already draining it.
func (s *scheduler) submit(ctx context.Context, req agentkit.LoopRequest) {
	sessionID := rctx.ConversationFromLoopRequest(req)

	var start bool
	s.withQueues(func(queues map[agentkit.SessionID]*sessionQueue) {
		queue := queues[sessionID]
		if queue == nil {
			queue = &sessionQueue{}
			queues[sessionID] = queue
		}
		queue.pending = append(queue.pending, req)
		start = !queue.running
		if start {
			queue.running = true
		}
	})

	if start {
		s.wg.Add(1)
		go s.drain(ctx, sessionID)
	}
}

// drain serves one session's backlog in order, then retires. Retiring through the
// same registry goroutine as submit keeps enqueue/dequeue safe without a mutex.
func (s *scheduler) drain(ctx context.Context, sessionID agentkit.SessionID) {
	defer s.wg.Done()
	for {
		var req agentkit.LoopRequest
		var empty bool
		s.withQueues(func(queues map[agentkit.SessionID]*sessionQueue) {
			queue := queues[sessionID]
			if queue == nil || len(queue.pending) == 0 {
				if queue != nil {
					queue.running = false
					delete(queues, sessionID)
				}
				empty = true
				return
			}
			req = queue.pending[0]
			queue.pending = queue.pending[1:]
		})
		if empty {
			return
		}

		if err := s.acquire(ctx); err != nil {
			return
		}
		err := s.dispatch(ctx, req)
		s.release()
		if err != nil && s.onError != nil {
			s.onError(ctx, req, err)
		}
	}
}

// wait blocks until every in-flight turn finishes, so shutdown does not cut a
// turn off before it records turn/end. A turn that ignores cancellation cannot
// hold the process forever: after timeout wait gives up and reports it.
// When timeout is 0, return immediately without waiting (in-flight work is abandoned).
func (s *scheduler) wait(timeout time.Duration) {
	if timeout == 0 {
		slog.Warn("shutdown: abandoning in-flight turns without waiting")
		return
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	if timeout < 0 {
		<-done
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("shutdown timeout exceeded, abandoning in-flight turns",
			"timeout", timeout.String(),
			"pending_sessions", s.pendingSessions(),
		)
	}
}

func (s *scheduler) pendingSessions() int {
	var n int
	s.withQueues(func(queues map[agentkit.SessionID]*sessionQueue) {
		n = len(queues)
	})
	return n
}
