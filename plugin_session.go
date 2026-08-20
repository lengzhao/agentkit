package agentkit

import "context"

// Session is the durable source of truth for model-visible state.
type Session interface {
	ID() SessionID
	Append(context.Context, SessionEvent) (EventSeq, error)
	Read(context.Context, EventSeq) ([]SessionEvent, error)
	DeriveMessages(context.Context) ([]ModelMessage, error)
}

type SessionQuery interface {
	Query(context.Context, SessionQueryRequest) (SessionQueryResult, error)
}

type SessionQueryRequest struct {
	SessionID SessionID
	Query     string
}

type SessionQueryResult struct {
	Events []SessionEvent
}
