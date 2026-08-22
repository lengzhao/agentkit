package agentkit

import "context"

// Loop is the turn scheduler. It routes inbound MessageEvents to agents,
// serializes work per SessionID, and owns per-session steer/follow-up control.
// Loop seeds ctx with KeySessionID/KeyAgentID/KeyPlatformID/KeySessionControl
// before calling the agent. It does not resolve Session objects; that is the
// agent's responsibility.
type Loop interface {
	Dispatch(context.Context, LoopRequest) error
	Steer(context.Context, ModelMessage) error
	FollowUp(context.Context, ModelMessage) error
}

// LoopRequest wraps one inbound message. Event.SessionID is required; Dispatch
// copies Event.SessionID, Event.AgentID and Event.PlatformID into context keys
// before invoking Agent.RunTurn.
type LoopRequest struct {
	Event MessageEvent
	Emit  OutboundEmit
}
