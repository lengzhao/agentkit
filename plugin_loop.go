package agentkit

import (
	"context"
)

// Loop is the turn scheduler. It routes inbound MessageEvents to agents,
// serializes work per SessionID, and owns per-session steer/follow-up control.
// Loop seeds ctx with KeySessionID/KeyDeliverySessionID/KeyStoreSessionID/
// KeyAgentID/KeyPlatformID/KeyUserID/KeyMessageMetadata/KeySessionControl/KeyOutboundEmit before
// calling the agent. It does not resolve Session objects; that is the agent's responsibility.
type Loop interface {
	Dispatch(context.Context, LoopRequest) error
	Steer(context.Context, ModelMessage) error
	FollowUp(context.Context, ModelMessage) error
	// IsSessionBusy reports whether a turn is currently executing for the session.
	IsSessionBusy(SessionID) bool
	// TryDeliverPermission consumes a typed permission reply. It returns true
	// when the message was handled and must not start a new turn.
	TryDeliverPermission(MessageEvent) bool
	// SupersedePendingForInbound cancels an active permission wait when a new
	// user message arrives without a permission reply.
	SupersedePendingForInbound(MessageEvent)
}

// LoopRequest wraps one inbound message. Runner rewrites Event.SessionID to the
// effective id (after sessionScope) before Dispatch; DeliverySessionID keeps the
// platform routing target for outbound Send.
type LoopRequest struct {
	Event             MessageEvent
	DeliverySessionID SessionID // platform delivery target; empty means Event.SessionID
	StoreSessionID    SessionID // logical history id; empty means Event.SessionID
	Emit              OutboundEmit
	Capability        any // permission.Capability, resolved by runner from the inbound platform
}
