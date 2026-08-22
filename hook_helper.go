package agentkit

import "context"

type beforeStepHook struct {
	order int
	fn    func(context.Context, *BeforeStep) error
}

func (h *beforeStepHook) Order() int { return h.order }
func (h *beforeStepHook) BeforeStep(ctx context.Context, in *BeforeStep) error {
	return h.fn(ctx, in)
}

// OnBeforeStep wraps a function as a BeforeStepHook.
func OnBeforeStep(order int, fn func(context.Context, *BeforeStep) error) BeforeStepHook {
	return &beforeStepHook{order: order, fn: fn}
}

type beforeToolHook struct {
	order int
	fn    func(context.Context, *ToolCall) error
}

func (h *beforeToolHook) Order() int { return h.order }
func (h *beforeToolHook) BeforeTool(ctx context.Context, in *ToolCall) error {
	return h.fn(ctx, in)
}

// OnBeforeTool wraps a function as a BeforeToolHook.
func OnBeforeTool(order int, fn func(context.Context, *ToolCall) error) BeforeToolHook {
	return &beforeToolHook{order: order, fn: fn}
}

type afterToolHook struct {
	order int
	fn    func(context.Context, *ToolResult) error
}

func (h *afterToolHook) Order() int { return h.order }
func (h *afterToolHook) AfterTool(ctx context.Context, in *ToolResult) error {
	return h.fn(ctx, in)
}

// OnAfterTool wraps a function as an AfterToolHook.
func OnAfterTool(order int, fn func(context.Context, *ToolResult) error) AfterToolHook {
	return &afterToolHook{order: order, fn: fn}
}

type HookRuntime interface {
	BeforeStep(context.Context, *BeforeStep) error
	BeforeTool(context.Context, *ToolCall) error
	AfterTool(context.Context, *ToolResult) error
}
