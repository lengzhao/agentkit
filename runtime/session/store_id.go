package session

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// AgentStoreSessionID is the session file agents read and append to during a
// turn. Runner keeps KeySessionID at the collapsed effective id for locking and
// tenant workspace, and sets KeyStoreSessionID when a stable entry key maps to a
// distinct logical history.
func AgentStoreSessionID(ctx context.Context) agentkit.SessionID {
	if storeID, ok := ctx.Value(agentkit.KeyStoreSessionID).(agentkit.SessionID); ok && storeID != "" {
		return storeID
	}
	effective, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	return effective
}
