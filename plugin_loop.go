package agentkit

import (
	"context"
)

// Loop is the turn scheduler. It routes inbound MessageEvents to agents,
// serializes work per SessionID, and owns per-session steer/follow-up control.
// Loop seeds ctx with KeySessionID/KeyAgentID/KeyPlatformID/KeyUserID/
// KeySessionControl/KeyOutboundEmit before calling the agent. It does not resolve Session objects; that is the agent's responsibility.
type Loop interface {
	Dispatch(context.Context, LoopRequest) error
	Steer(context.Context, ModelMessage) error
	FollowUp(context.Context, ModelMessage) error
	// TryDeliverPermission consumes a typed permission reply. It returns true
	// when the message was handled and must not start a new turn.
	TryDeliverPermission(MessageEvent) bool
	// SupersedePendingForInbound cancels an active permission wait when a new
	// user message arrives without a permission reply.
	SupersedePendingForInbound(MessageEvent)
}

// LoopRequest wraps one inbound message. Event.SessionID is required; Dispatch
// copies Event.SessionID, Event.AgentID, Event.PlatformID and Event.UserID into
// context keys before invoking Agent.RunTurn.
type LoopRequest struct {
	Event      MessageEvent
	Emit       OutboundEmit
	Capability any // permission.Capability, resolved by runner from the inbound platform
}
