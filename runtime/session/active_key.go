package session

import (
	"github.com/lengzhao/agentkit"
)

// ActiveSessionEntryKey returns the stable session-store key used for /new
// active-session mapping.
func ActiveSessionEntryKey(platform string, delivery agentkit.SessionID, scope SessionScope, userID string) agentkit.SessionID {
	if delivery == "" {
		return ""
	}
	policy := RoutePolicyForPlatform(platform, DefaultRoutePolicy(scope))
	return ActiveEntryKey(SessionRouteFromDelivery(platform, delivery, ""), policy, userID)
}
