package agentkit

import "context"

// Session is the durable source of truth for model-visible state.
type Session interface {
	ID() SessionID
	Append(context.Context, SessionEvent) (EventSeq, error)
	Read(context.Context, EventSeq) ([]SessionEvent, error)
	DeriveMessages(context.Context) ([]ModelMessage, error)
}

// SessionStore resolves durable sessions by ID. IM platforms (Slack, 飞书等)
// assign one SessionID per channel/thread; Loop uses the store to route turns.
type SessionStore interface {
	Get(context.Context, SessionID) (Session, error)
}
