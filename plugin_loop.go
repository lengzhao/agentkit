package agentkit

import (
	"context"
)

// Loop is the turn scheduler. It routes inbound MessageEvents to agents,
// serializes work per Conversation, and owns per-session steer/follow-up control.
// Loop seeds ctx with KeyTurnEnvelope (conversation, agent, route, workspace) and
// KeyOutboundEmit before calling the agent. It does not resolve Session objects; that is the agent's responsibility.
type Loop interface {
	Dispatch(context.Context, LoopRequest) error
	Steer(context.Context, ModelMessage) error
	FollowUp(context.Context, ModelMessage) error
	// Cancel requests the in-flight turn for the session in ctx to stop. The
	// conversation key is read from TurnEnvelope, same as Steer/FollowUp.
	Cancel(context.Context, string) error
	// IsSessionBusy reports whether a turn is currently executing for the session.
	IsSessionBusy(SessionID) bool
	// TryDeliverPermission consumes a typed permission reply. It returns true
	// when the message was handled and must not start a new turn.
	TryDeliverPermission(MessageEvent) bool
	// SupersedePendingForInbound cancels an active permission wait when a new
	// user message arrives without a permission reply.
	SupersedePendingForInbound(MessageEvent)
}

// LoopRequest wraps one inbound message. Runner resolves TurnEnvelope on
// Event.Envelope before Dispatch: Conversation for history/lock, Route for outbound Send.
type LoopRequest struct {
	Event      MessageEvent
	Emit       OutboundEmit
	Capability any // permission.Capability, resolved by runner from the inbound platform
}
