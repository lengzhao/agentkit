package session

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// ActiveSessionEntryKey returns the stable session-store key used for /new
// active-session mapping. IM platforms use the effective id after sessionScope;
// chat-api keeps the per-conversation delivery id.
func ActiveSessionEntryKey(platform string, delivery agentkit.SessionID, scope SessionScope, userID string) agentkit.SessionID {
	if delivery == "" {
		return ""
	}
	if strings.TrimSpace(platform) == "chat-api" {
		return delivery
	}
	return ApplyScope(delivery, scope, userID)
}
