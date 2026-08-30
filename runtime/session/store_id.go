package session

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// AgentStoreSessionID is the session file agents read and append to during a
// turn. Runner keeps KeySessionID at the collapsed effective id for locking and
// tenant workspace, and sets KeyStoreSessionID when a stable entry key maps to a
// distinct logical history.
//
// Inside a subagent, KeySessionID already points at the child sub:… session.
// Ignore KeyStoreSessionID there so a parent chat-api mapping cannot redirect
// the child's turn lifecycle or recovery onto the parent's open delegate turn.
func AgentStoreSessionID(ctx context.Context) agentkit.SessionID {
	if ctx.Value(agentkit.KeyInSubagent) != nil || ctx.Value(agentkit.KeyScheduleStateless) != nil {
		if id, ok := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID); ok && id != "" {
			return id
		}
	}
	if storeID, ok := ctx.Value(agentkit.KeyStoreSessionID).(agentkit.SessionID); ok && storeID != "" {
		return storeID
	}
	effective, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	return effective
}
