package agentkit

import "context"

type HookProvider interface {
	Hooks() []Hook
}

// Hook marks a value that may implement one or more hook point interfaces.
// Execution order follows deps.providers list order; within a provider, Hooks()
// slice order is preserved.
type Hook interface {
	isHook()
}

type BeforeStepHook interface {
	Hook
	BeforeStep(context.Context, *BeforeStep) error
}

type BeforeToolHook interface {
	Hook
	BeforeTool(context.Context, *ToolCall) error
}

type AfterToolHook interface {
	Hook
	AfterTool(context.Context, *ToolResult) error
}

type TurnStoppingHook interface {
	Hook
	TurnStopping(context.Context, *TurnStopping) error
}

// TurnCompleteHook runs after a turn finishes successfully (turn/end recorded, not cancelled).
// Implementations should return quickly and offload heavy work to a background goroutine.
type TurnCompleteHook interface {
	Hook
	TurnComplete(context.Context, *TurnComplete) error
}

// TurnComplete is the post-turn snapshot for background learning and review forks.
type TurnComplete struct {
	AgentID   AgentID
	SessionID SessionID
	Model     string
	// Steps is the number of model steps the turn ran across all segments.
	Steps int
	// Segments is the number of continuations the turn ran (0 for a
	// single-segment turn), matching turn/continue's Segment numbering.
	Segments int
	// TurnTokens is total model tokens recorded for this turn (0 when unknown).
	TurnTokens int
	// Messages is the derived model-visible history at turn end (read-only).
	Messages []ModelMessage
}

// BeforeStep is invoked before a model step. Hooks read routing context from
// ctx.Value(KeyTurnEnvelope) / SessionIDFromContext; hooks that need durable
// state should depend on SessionStore via pluginkit Deps.
type BeforeStep struct {
	// Step is the 0-based, turn-wide index of the step about to run; it matches
	// the step/start event index and equals the number of steps completed so
	// far in this turn.
	Step int
	// Segment is the 0-based continuation segment the step belongs to (0 for
	// the initial segment), matching turn/continue's Segment numbering.
	Segment  int
	Messages []ModelMessage
}

// TurnStopReason explains why the agent reached the end of a turn segment.
type TurnStopReason string

const (
	// StopNoToolCalls means the assistant answered without requesting tools.
	StopNoToolCalls TurnStopReason = "no-tool-calls"
)

// TurnStopping is invoked when the agent is about to end a turn segment. Hooks
// may append Continue messages to extend the turn with another segment, or set
// Stop to force the turn to end. Stop wins over Continue.
type TurnStopping struct {
	Reason   TurnStopReason
	Steps    int
	Segments int
	Tokens   int
	// Messages is the derived history at the stopping point. Read-only for hooks.
	Messages []ModelMessage
	// Continue holds messages that extend the turn. Hooks append to it.
	Continue   []ModelMessage
	Stop       bool
	StopReason string
}
