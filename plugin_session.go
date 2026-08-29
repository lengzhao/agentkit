package agentkit

import "context"

// Session is the durable source of truth for model-visible state.
type Session interface {
	ID() SessionID
	Append(context.Context, SessionEvent) (EventSeq, error)
	Read(context.Context, EventSeq) ([]SessionEvent, error)
	DeriveMessages(context.Context) ([]ModelMessage, error)
}

// SessionStore resolves durable sessions by opaque SessionID. Platform plugins
// generate SessionID values (cc-connect style: platform:segment:...); agent
// plugins depend on SessionStore and call Get with the logical history id
// resolved for the turn. Loop only uses SessionID for routing and per-session
// locking, never SessionStore.Get.
type SessionStore interface {
	Get(context.Context, SessionID) (Session, error)
}

// ActiveSessionStore maps stable platform/effective session keys to the logical
// session currently used for model-visible history. Missing mapping means the
// key itself is the logical session.
type ActiveSessionStore interface {
	ActiveSession(context.Context, SessionID) (SessionID, error)
	SetActiveSession(context.Context, SessionID, SessionID) error
}
