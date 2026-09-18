package rctx

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// WithSession attaches the turn's open session for tool handlers (e.g. delegate).
func WithSession(ctx context.Context, s agentkit.Session) context.Context {
	if s == nil {
		return ctx
	}
	return context.WithValue(ctx, agentkit.KeySession, s)
}

// SessionFromContext returns the open session when the agent set it for tools.
func SessionFromContext(ctx context.Context) (agentkit.Session, bool) {
	s, ok := ctx.Value(agentkit.KeySession).(agentkit.Session)
	return s, ok && s != nil
}
