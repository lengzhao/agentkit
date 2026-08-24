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

// BeforeStep is invoked before a model step. Hooks read routing context from
// ctx.Value(KeySessionID) / ctx.Value(KeyAgentID); hooks that need durable
// state should depend on SessionStore via pluginkit Deps.
type BeforeStep struct {
	Messages []ModelMessage
}
