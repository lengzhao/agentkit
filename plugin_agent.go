package agentkit

import "context"

// FollowUpMode controls how queued follow-up messages are drained after a turn.
type FollowUpMode string

const (
	FollowUpOneAtATime FollowUpMode = "one-at-a-time"
	FollowUpAll        FollowUpMode = "all"
)

// Agent is the execution unit below Loop. It owns session, prompt, model,
// tools, policies and hooks for a single agent identity.
type Agent interface {
	ID() AgentID
	Session() Session
	RunTurn(context.Context, TurnInput) (TurnResult, error)
}

// SessionControl steers or queues follow-ups for one session.
type SessionControl interface {
	Steer(context.Context, ModelMessage) error
	FollowUp(context.Context, ModelMessage) error
	Cancel(context.Context, string) error
	DrainFollowUps(context.Context, FollowUpMode) ([]ModelMessage, error)
}

// SessionControlRequest targets an in-flight or idle agent session.
type SessionControlRequest struct {
	AgentID   AgentID
	SessionID SessionID
	Message   ModelMessage
}

type TurnInput struct {
	Message ModelMessage
	Emit    OutboundEmit
	// Session overrides the agent's default session when set (multi-session / IM).
	Session Session
	// Control is session-scoped steer/follow-up state; Loop supplies this per
	// Dispatch. Implementations that also expose step-level control (see
	// runtime/sessioncontrol.TurnControl) enable steer interrupts mid-turn.
	Control SessionControl
}

type TurnResult struct {
	Messages []ModelMessage
	Events   []SessionEvent
}
