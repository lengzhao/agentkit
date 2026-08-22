package agentkit

import "context"

type HookProvider interface {
	Hooks() []Hook
}

// Hook is the base type for all hook implementations. The concrete hook point
// is determined by which of the *Hook interfaces below the value satisfies;
// Order decides the relative execution order within one point.
type Hook interface {
	Order() int
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

type BeforeStep struct {
	SessionID SessionID
	AgentID   AgentID
	Session   Session
	Messages  []ModelMessage
}
