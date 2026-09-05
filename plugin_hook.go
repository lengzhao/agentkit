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

// BeforeStep is invoked before a model step. Hooks read routing context from
// ctx.Value(KeyTurnEnvelope) / SessionIDFromContext; hooks that need durable
// state should depend on SessionStore via pluginkit Deps.
type BeforeStep struct {
	Messages []ModelMessage
}

// TurnStopReason explains why the agent reached the end of a turn segment.
type TurnStopReason string

const (
	// StopNoToolCalls means the assistant answered without requesting tools.
	StopNoToolCalls TurnStopReason = "no-tool-calls"
	// StopStepLimit means the per-segment step allowance ran out.
	StopStepLimit TurnStopReason = "step-limit"
	// StopBudget means a hard run budget is exhausted; Continue is ignored.
	StopBudget TurnStopReason = "budget"
)

// TurnStopping is invoked when the agent is about to end a turn. Hooks may
// append Continue messages to extend the turn with another segment, or set Stop
// to force the turn to end. Stop wins over Continue, and the agent ignores
// Continue when Budget.Exhausted is true: no hook can outrun a hard budget.
type TurnStopping struct {
	Reason   TurnStopReason
	Steps    int
	Segments int
	Budget   BudgetState
	// Messages is the derived history at the stopping point. Read-only for hooks.
	Messages []ModelMessage
	// Continue holds messages that extend the turn. Hooks append to it.
	Continue   []ModelMessage
	Stop       bool
	StopReason string
}

// BudgetState reports what is left of the run budget. Unlimited dimensions
// report -1 so hooks can distinguish "no limit" from "nothing left".
type BudgetState struct {
	RemainingSteps         int
	RemainingContinuations int
	RemainingSeconds       int
	RemainingTokens        int
	// SoftExhausted is true once any limited dimension crosses softRatio.
	SoftExhausted bool
	// Exhausted is true when a hard limit is reached; Continue is then ignored.
	Exhausted bool
}
