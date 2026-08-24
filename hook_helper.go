package agentkit

import "context"

type beforeStepHook struct {
	fn func(context.Context, *BeforeStep) error
}

func (h *beforeStepHook) isHook() {}

func (h *beforeStepHook) BeforeStep(ctx context.Context, in *BeforeStep) error {
	return h.fn(ctx, in)
}

// OnBeforeStep wraps a function as a BeforeStepHook.
func OnBeforeStep(fn func(context.Context, *BeforeStep) error) BeforeStepHook {
	return &beforeStepHook{fn: fn}
}

type beforeToolHook struct {
	fn func(context.Context, *ToolCall) error
}

func (h *beforeToolHook) isHook() {}

func (h *beforeToolHook) BeforeTool(ctx context.Context, in *ToolCall) error {
	return h.fn(ctx, in)
}

// OnBeforeTool wraps a function as a BeforeToolHook.
func OnBeforeTool(fn func(context.Context, *ToolCall) error) BeforeToolHook {
	return &beforeToolHook{fn: fn}
}

type afterToolHook struct {
	fn func(context.Context, *ToolResult) error
}

func (h *afterToolHook) isHook() {}

func (h *afterToolHook) AfterTool(ctx context.Context, in *ToolResult) error {
	return h.fn(ctx, in)
}

// OnAfterTool wraps a function as an AfterToolHook.
func OnAfterTool(fn func(context.Context, *ToolResult) error) AfterToolHook {
	return &afterToolHook{fn: fn}
}

type turnStoppingHook struct {
	fn func(context.Context, *TurnStopping) error
}

func (h *turnStoppingHook) isHook() {}

func (h *turnStoppingHook) TurnStopping(ctx context.Context, in *TurnStopping) error {
	return h.fn(ctx, in)
}

// OnTurnStopping wraps a function as a TurnStoppingHook.
func OnTurnStopping(fn func(context.Context, *TurnStopping) error) TurnStoppingHook {
	return &turnStoppingHook{fn: fn}
}

type HookRuntime interface {
	BeforeStep(context.Context, *BeforeStep) error
	BeforeTool(context.Context, *ToolCall) error
	AfterTool(context.Context, *ToolResult) error
	TurnStopping(context.Context, *TurnStopping) error
}
