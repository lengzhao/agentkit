package session

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// AgentStoreSessionID is the session file agents read and append to during a
// turn. Runner keeps KeySessionID at the collapsed effective id for locking and
// tenant workspace; chat-api additionally emits a per-conversation delivery id
// that must not be folded away or new HTTP conversations inherit the whole
// channel's agent history.
func AgentStoreSessionID(ctx context.Context) agentkit.SessionID {
	effective, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	platformID, _ := ctx.Value(agentkit.KeyPlatformID).(string)
	if platformID == "chat-api" {
		if delivery, ok := ctx.Value(agentkit.KeyDeliverySessionID).(agentkit.SessionID); ok && delivery != "" {
			return delivery
		}
	}
	return effective
}
