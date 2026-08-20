package agentkit

import "context"

type beforeStepHook struct {
	order int
	fn    func(context.Context, *BeforeStep) error
}

func (h *beforeStepHook) Point() HookPoint { return HookBeforeStep }
func (h *beforeStepHook) Order() int       { return h.order }
func (h *beforeStepHook) BeforeStep(ctx context.Context, in *BeforeStep) error {
	return h.fn(ctx, in)
}

// OnBeforeStep wraps a function as a BeforeStepHook.
func OnBeforeStep(order int, fn func(context.Context, *BeforeStep) error) BeforeStepHook {
	return &beforeStepHook{order: order, fn: fn}
}

type HookRuntime interface {
	BeforeStep(context.Context, *BeforeStep) error
}
