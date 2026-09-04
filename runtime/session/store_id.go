package session

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// AgentStoreSessionID is the session file agents read and append to during a
// turn. Prefer agentkit.HistorySessionID; Loop pre-resolves KeyHistorySessionID.
func AgentStoreSessionID(ctx context.Context) agentkit.SessionID {
	return agentkit.HistorySessionID(ctx)
}
