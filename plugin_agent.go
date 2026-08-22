package agentkit

import "context"

// Agent is the execution unit below Loop. It owns session, prompt, model,
// tools, policies and hooks for a single agent identity.
type Agent interface {
	ID() AgentID
	Session() Session
	RunTurn(context.Context, TurnInput) (TurnResult, error)
	Steer(context.Context, ModelMessage) error
	FollowUp(context.Context, ModelMessage) error
	Cancel(context.Context, string) error
	WhenIdle(context.Context) error
}

type TurnInput struct {
	Message ModelMessage
	Emit    OutboundEmit
	// Session overrides the agent's default session when set (multi-session / IM).
	Session Session
}

type TurnResult struct {
	Messages []ModelMessage
	Events   []SessionEvent
}
