package runner

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

// scheduler runs turns from distinct sessions in parallel while keeping each
// session's own messages in arrival order.
//
// Loop already serializes per SessionID, but it does so with a plain mutex, and
// Go mutexes are not FIFO: handing it two concurrent messages for one session
// could run them out of order. So ordering is enforced here, with one worker per
// session drained in order.
//
// Concurrency is capped by a slot semaphore with one invariant: every submitted
// request carries exactly one slot, released when its dispatch finishes. The
// intake acquires the slot before reading the next event, so
//
//	in-flight + queued <= maxConcurrent
//
// and a queued request never has to wait for a slot to run, which is what keeps
// the design deadlock-free. It also means read-ahead from the platform is
// bounded: at maxConcurrent == 1 the intake blocks until the previous turn is
// done, exactly as a fully serial loop would.
type scheduler struct {
	dispatch func(context.Context, agentkit.LoopRequest) error
	onError  func(context.Context, agentkit.LoopRequest, error)
	slots    chan struct{}

	mu     sync.Mutex
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
	return &scheduler{
		dispatch: dispatch,
		onError:  onError,
		slots:    make(chan struct{}, maxConcurrent),
		queues:   make(map[agentkit.SessionID]*sessionQueue),
	}
}

// acquire takes the slot that the next submitted request will carry.
func (s *scheduler) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.slots <- struct{}{}:
		return nil
	}
}

// release returns a slot taken by acquire when no request was submitted after
// all, e.g. the platform reported EOF or an empty event.
func (s *scheduler) release() {
	select {
	case <-s.slots:
	default:
	}
}

// submit queues a request whose slot is already held, starting a worker for the
// session when one is not already draining it.
func (s *scheduler) submit(ctx context.Context, req agentkit.LoopRequest) {
	sessionID := req.Event.SessionID

	s.mu.Lock()
	queue := s.queues[sessionID]
	if queue == nil {
		queue = &sessionQueue{}
		s.queues[sessionID] = queue
	}
	queue.pending = append(queue.pending, req)
	start := !queue.running
	if start {
		queue.running = true
		s.wg.Add(1)
	}
	s.mu.Unlock()

	if start {
		go s.drain(ctx, sessionID)
	}
}

// drain serves one session's backlog in order, then retires. Retiring under the
// same lock that submit uses is what makes it safe: a request enqueued
// concurrently either finds this worker still running, or creates a fresh one.
func (s *scheduler) drain(ctx context.Context, sessionID agentkit.SessionID) {
	defer s.wg.Done()
	for {
		s.mu.Lock()
		queue := s.queues[sessionID]
		if queue == nil || len(queue.pending) == 0 {
			if queue != nil {
				queue.running = false
				// Drop the entry so a daemon serving many short-lived sessions
				// does not accumulate them forever.
				delete(s.queues, sessionID)
			}
			s.mu.Unlock()
			return
		}
		req := queue.pending[0]
		queue.pending = queue.pending[1:]
		s.mu.Unlock()

		err := s.dispatch(ctx, req)
		// The slot travelled with this request; hand it back before reporting so
		// the intake can move on.
		s.release()
		if err != nil && s.onError != nil {
			s.onError(ctx, req, err)
		}
	}
}

// wait blocks until every in-flight turn finishes, so shutdown does not cut a
// turn off before it records turn/end. A turn that ignores cancellation cannot
// hold the process forever: after timeout wait gives up and reports it.
func (s *scheduler) wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	if timeout <= 0 {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queues)
}
