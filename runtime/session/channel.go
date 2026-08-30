package session

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/tenant"
)

// ChannelKeyFromContext derives the channel-scoped key for the current turn.
// It collapses delivery to channel scope so scheduled jobs can be isolated per IM channel.
func ChannelKeyFromContext(ctx context.Context) string {
	delivery, _ := ctx.Value(agentkit.KeyDeliverySessionID).(agentkit.SessionID)
	if delivery == "" {
		delivery, _ = ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	}
	userID, _ := ctx.Value(agentkit.KeyUserID).(string)
	scoped := ApplyScope(delivery, ScopeChannel, userID)
	return tenant.Key(string(scoped))
}

// ChannelKeyMatches reports whether a job belongs to the given channel key.
func ChannelKeyMatches(jobChannelKey, contextChannelKey string) bool {
	return strings.TrimSpace(jobChannelKey) == strings.TrimSpace(contextChannelKey)
}
