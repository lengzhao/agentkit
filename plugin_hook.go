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

// BeforeStep is invoked before a model step. Hooks read routing context from
// ctx.Value(KeySessionID) / ctx.Value(KeyAgentID); hooks that need durable
// state should depend on SessionStore via pluginkit Deps.
type BeforeStep struct {
	Messages []ModelMessage
}
