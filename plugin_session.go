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
// plugins depend on SessionStore and call Get with ctx.Value(KeySessionID)
// during RunTurn. Loop only uses SessionID for routing and per-session locking,
// never SessionStore.Get.
type SessionStore interface {
	Get(context.Context, SessionID) (Session, error)
}
