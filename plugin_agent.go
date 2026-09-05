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
// SessionIDFromContext. Loop does not inject Session objects or duplicate
// routing fields in TurnInput.
//
// Steer/follow-up/cancel are owned by Loop per conversation. Loop seeds
// ctx.Value(KeySessionControl) before RunTurn; Agent reads it to inject
// queued steering messages at step boundaries without interrupting in-flight steps.
//
// Agent plugins declare SessionStore in their Deps struct (pluginkit injection).
type Agent interface {
	ID() AgentID
	RunTurn(context.Context, TurnInput) error
}

// AgentCatalogEntry optionally describes a built agent for /agent help output.
type AgentCatalogEntry interface {
	AgentCatalogEntry() string
}

// TurnInput carries one turn's payload. Routing context is available through
// TurnEnvelope on ctx (SessionIDFromContext, AgentIDFromContext, PlatformFromContext, UserIDFromContext).
type TurnInput struct {
	Message ModelMessage
	Emit    OutboundEmit
}
