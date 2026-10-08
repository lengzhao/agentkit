package llm

import (
	"context"
	"strings"
)

type actualModelKey struct{}

// modelSlot holds the catalog model id used for the successful LLM stream in this step.
type modelSlot struct {
	actual string
}

// WithActualModelSlot attaches a per-step slot for recording the model that actually served the stream.
// Call at the start of each agent step before Stream; providers call NoteActualModel on success.
func WithActualModelSlot(ctx context.Context) context.Context {
	if _, ok := ctx.Value(actualModelKey{}).(*modelSlot); ok {
		return ctx
	}
	return context.WithValue(ctx, actualModelKey{}, &modelSlot{})
}

// NoteActualModel records the model id for the active LLM attempt (last non-empty write wins).
func NoteActualModel(ctx context.Context, model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	slot, _ := ctx.Value(actualModelKey{}).(*modelSlot)
	if slot == nil {
		return
	}
	slot.actual = model
}

// ActualModelFrom returns the model id noted for the current step, or "" when unset.
func ActualModelFrom(ctx context.Context) string {
	slot, _ := ctx.Value(actualModelKey{}).(*modelSlot)
	if slot == nil {
		return ""
	}
	return strings.TrimSpace(slot.actual)
}
