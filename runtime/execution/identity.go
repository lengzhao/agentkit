package execution

import (
	"context"

	"github.com/google/uuid"
	"github.com/lengzhao/agentkit"
	capexecution "github.com/lengzhao/agentkit/cap/execution"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// NewIdentity assigns an invocation independent of its history and backend turn.
// Hosts without a root turn start a new ownership domain.
func NewIdentity(parent context.Context) capexecution.Identity {
	p := rctx.ExecutionIdentityFromContext(parent)
	identity := capexecution.Identity{ID: uuid.NewString(), ParentID: p.ID, RootTurnID: p.RootTurnID}
	if identity.RootTurnID == "" {
		identity.RootTurnID, _ = parent.Value(agentkit.KeyTurnID).(string)
	}
	if identity.RootTurnID == "" {
		identity.RootTurnID = identity.ID
	}
	return identity
}
