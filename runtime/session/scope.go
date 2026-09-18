package session

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// DefaultSessionScope is the runner default when sessionScope is unset.
const DefaultSessionScope = agentkit.DefaultSessionScope

// SessionScope selects how delivery SessionIDs collapse for Loop scheduling
// and session history.
type SessionScope = agentkit.SessionScope

const (
	ScopeChannel = agentkit.SessionScopeChannel
	ScopeThread  = agentkit.SessionScopeThread
	ScopeUser    = agentkit.SessionScopeUser
)

// DeliveryParts holds parsed segments of a platform delivery SessionID.
type DeliveryParts = rctx.DeliveryParts
