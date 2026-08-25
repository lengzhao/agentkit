package agentkit

import "context"

// FollowUpMode controls how queued follow-up messages are drained after a turn.
type FollowUpMode string

const (
	FollowUpOneAtATime FollowUpMode = "one-at-a-time"
	FollowUpAll        FollowUpMode = "all"
)

// Agent is the execution unit below Loop. It owns prompt, model, tools,
// policies and hooks for a single agent identity. Conversation state lives in
// Session; the agent implementation resolves Session via SessionStore using
// ctx.Value(KeySessionID). Loop does not inject Session objects or duplicate
// routing fields in TurnInput.
//
// Steer/follow-up/cancel are owned by Loop per SessionID. Loop seeds
// ctx.Value(KeySessionControl) before RunTurn; Agent reads it for step-level
// steer interrupts.
//
// Agent plugins declare SessionStore in their Deps struct (pluginkit injection).
type Agent interface {
	ID() AgentID
	RunTurn(context.Context, TurnInput) error
}

// TurnInput carries one turn's payload. Routing context is available through
// ctx.Value(KeySessionID), ctx.Value(KeyAgentID), ctx.Value(KeyUserID), and
// related agentkit keys.
type TurnInput struct {
	Message ModelMessage
	Emit    OutboundEmit
}
