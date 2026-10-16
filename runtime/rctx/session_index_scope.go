package rctx

import "context"

type sessionIndexGlobalKey struct{}

// WithSessionIndexGlobal opts session-index queries out of channel (workspace)
// scoping: they span every tenant sharing the backing store. Only stores with
// a global view (session/sql-index) honor it; file-based indexes are
// physically per-workspace and ignore the flag.
func WithSessionIndexGlobal(ctx context.Context) context.Context {
	return context.WithValue(ctx, sessionIndexGlobalKey{}, true)
}

// SessionIndexGlobalFromContext reports whether global index scope was requested.
func SessionIndexGlobalFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(sessionIndexGlobalKey{}).(bool)
	return v
}
