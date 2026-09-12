package learning

import (
	"context"
	"sync"
)

// ReviewRunRegistry cancels in-flight background reviews (per session + global shutdown).
type ReviewRunRegistry struct {
	mu      sync.Mutex
	active  map[string]context.CancelFunc
	onEmpty func()
}

var globalReviewRuns = &ReviewRunRegistry{}

// GlobalReviewRuns is the process-wide registry used by hook/background-review.
func GlobalReviewRuns() *ReviewRunRegistry {
	return globalReviewRuns
}

// RegisterGlobalReviewRuns wires an alternate registry into process shutdown (tests only).
// Production code uses GlobalReviewRuns.
func RegisterGlobalReviewRuns(r *ReviewRunRegistry) {
	if r == nil {
		return
	}
	globalReviewRuns = r
}

// CancelAllBackgroundReviews stops every in-flight review (runner shutdown).
func CancelAllBackgroundReviews() {
	globalReviewRuns.CancelAll()
}

func (r *ReviewRunRegistry) Begin(parent context.Context, sessionKey string) (context.Context, context.CancelFunc) {
	r.mu.Lock()
	if r.active == nil {
		r.active = make(map[string]context.CancelFunc)
	}
	if prev, ok := r.active[sessionKey]; ok && prev != nil {
		prev()
	}
	ctx, cancel := context.WithCancel(parent)
	r.active[sessionKey] = cancel
	r.mu.Unlock()
	return ctx, cancel
}

func (r *ReviewRunRegistry) End(sessionKey string) {
	r.mu.Lock()
	if r.active != nil {
		delete(r.active, sessionKey)
	}
	r.mu.Unlock()
}

func (r *ReviewRunRegistry) CancelAll() {
	r.mu.Lock()
	for _, cancel := range r.active {
		if cancel != nil {
			cancel()
		}
	}
	r.active = make(map[string]context.CancelFunc)
	r.mu.Unlock()
}
