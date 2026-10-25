package agentkit

import "context"

// SteerAck reports the final disposition of one steered message so the
// transport that enqueued it can reflect it on the very message that was folded
// in. outcome is one of the cap/session SteerOutcome* values (queued, injected,
// next-turn). Implementations must tolerate being called at most once and must
// not block the caller: run any platform I/O on its own goroutine.
type SteerAck func(outcome string)

// SteerEntry is one queued steering message together with its outcome reporter.
// Ack is nil for callers that do not track per-message outcomes.
type SteerEntry struct {
	Message ModelMessage
	Ack     SteerAck
}

// SteerReporter is an optional Loop capability. A Loop that implements it
// accepts per-message steering acks, so a transport can learn whether a folded
// message was injected into the running turn or deferred to the session's next
// turn. Runners fall back to plain Loop.Steer when a loop does not implement it.
type SteerReporter interface {
	Loop
	// SteerEntry queues one steering message with its outcome reporter.
	SteerEntry(context.Context, SteerEntry) error
}