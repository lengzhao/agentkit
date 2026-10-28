package permission

import (
	"context"

	"github.com/google/uuid"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// ParentBroker forwards child approvals to the captured parent interaction.
// It owns correlation and delivery, never the parent broker's lifecycle.
type ParentBroker struct {
	parent     permission.Broker
	envelope   agentkit.TurnEnvelope
	agentID    agentkit.AgentID
	emit       agentkit.OutboundEmit
	capability permission.Capability
}

func NewParentBroker(parent context.Context, agentID agentkit.AgentID) *ParentBroker {
	broker, _ := permission.BrokerFrom(parent)
	if scoped, ok := broker.(interface{ ParentPermissionBroker() *ParentBroker }); ok {
		// Preserve the root approval route and actual deepest child identity.
		if upstream := scoped.ParentPermissionBroker(); upstream != nil {
			copy := *upstream
			copy.agentID = agentID
			return &copy
		}
	}
	return &ParentBroker{parent: broker, envelope: rctx.EnvelopeFromContext(parent), agentID: agentID,
		emit: rctx.OutboundEmitFromContext(parent), capability: permission.CapabilityFrom(parent)}
}

func (b *ParentBroker) PermissionCapability() permission.Capability { return b.capability }

// ParentPermissionBroker also supports direct composition of approval proxies.
func (b *ParentBroker) ParentPermissionBroker() *ParentBroker { return b }

func (b *ParentBroker) Await(ctx context.Context, req permission.Request) (permission.Result, error) {
	if err := ctx.Err(); err != nil {
		return permission.Result{}, err
	}
	if b.parent == nil {
		return permission.Result{Outcome: permission.OutcomeNoHuman, Reason: "child execution has no parent permission broker"}, nil
	}
	env := rctx.EnvelopeFromContext(ctx)
	env.Conversation = b.envelope.Conversation
	env.Workspace = b.envelope.Workspace
	env.Route = b.envelope.Route
	env.Actor = b.envelope.Actor
	env.AgentID = b.agentID
	ctx = rctx.ApplyEnvelopeToContext(ctx, env)
	ctx = rctx.ContextWithOutboundEmit(ctx, b.emit)
	// Backend IDs can repeat after reconnecting. UI correlation is host-owned.
	req.ID = uuid.NewString()
	return b.parent.Await(ctx, req)
}
