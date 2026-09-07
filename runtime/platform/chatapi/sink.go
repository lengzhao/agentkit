package chatapi

import (
	"time"
)

// runEventSink is the live SSE connection or a detached virtual sink.
type runEventSink interface {
	Event(name string, payload any) error
	Active() bool
}

type sseEventSink struct {
	w *sseWriter
}

func (s *sseEventSink) Event(name string, payload any) error {
	if s == nil || s.w == nil {
		return nil
	}
	return s.w.Event(name, payload)
}

func (s *sseEventSink) Active() bool { return s != nil && s.w != nil }

type detachedEventSink struct {
	run *runState
}

func (d *detachedEventSink) Active() bool { return false }

func (d *detachedEventSink) Event(name string, payload any) error {
	if d == nil || d.run == nil {
		return nil
	}
	if isRecoverableSSEEvent(name) {
		d.run.setLastRecoverable(name, payload)
	}
	return nil
}

func isRecoverableSSEEvent(name string) bool {
	switch name {
	case "text_delta", "thinking_delta", "question_request", "permission_request":
		return true
	default:
		return false
	}
}

type recoverableEvent struct {
	name      string
	payload   any
	createdAt time.Time
}

func (r *runState) setLastRecoverable(name string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Unresolved interaction must not be overwritten by later content deltas.
	if name == "text_delta" || name == "thinking_delta" {
		if ev := r.lastRecoverableEvent; ev != nil &&
			(ev.name == "question_request" || ev.name == "permission_request") {
			if r.interaction != nil && !r.interaction.Responded {
				return
			}
		}
	}
	r.lastRecoverableEvent = &recoverableEvent{
		name:      name,
		payload:   payload,
		createdAt: time.Now(),
	}
}

func (r *runState) peekLastRecoverable() *recoverableEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastRecoverableEvent
}

func (r *runState) clearLastRecoverable() {
	r.mu.Lock()
	r.lastRecoverableEvent = nil
	r.mu.Unlock()
}
