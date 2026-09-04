package agentkit

import "context"

const (
	// KeyHistorySessionID is the resolved session id agents use to append/read
	// durable history. Loop sets it before Agent.RunTurn; subagent dispatch sets
	// it for child turns. Prefer HistorySessionID(ctx) over reading raw keys.
	KeyHistorySessionID contextKey = "agentkit.history_session_id"
)

// ResolveHistorySessionID derives the durable history session id from context
// keys already present on ctx. Loop uses this when seeding KeyHistorySessionID.
func ResolveHistorySessionID(ctx context.Context) SessionID {
	if ctx.Value(KeyInSubagent) != nil || ctx.Value(KeyScheduleStateless) != nil {
		if id, ok := ctx.Value(KeySessionID).(SessionID); ok && id != "" {
			return id
		}
	}
	if storeID, ok := ctx.Value(KeyStoreSessionID).(SessionID); ok && storeID != "" {
		return storeID
	}
	effective, _ := ctx.Value(KeySessionID).(SessionID)
	return effective
}

// HistorySessionID returns the session id agents should use for durable history.
// It prefers the pre-resolved KeyHistorySessionID and falls back to
// ResolveHistorySessionID for tests and direct RunTurn callers.
func HistorySessionID(ctx context.Context) SessionID {
	if id, ok := ctx.Value(KeyHistorySessionID).(SessionID); ok && id != "" {
		return id
	}
	return ResolveHistorySessionID(ctx)
}
