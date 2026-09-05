package telemetry

import (
	"context"

	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
)

type ctxKey int

const (
	keyExporter ctxKey = iota
	keyTurnID
	keyToolParent
	keyScopeParent
)

// WithExporter stores an exporter on the context for downstream runtime code.
func WithExporter(ctx context.Context, exp captelemetry.Exporter) context.Context {
	if exp == nil {
		return ctx
	}
	return context.WithValue(ctx, keyExporter, exp)
}

// ExporterFrom returns the exporter on ctx, or Noop when unset.
func ExporterFrom(ctx context.Context) captelemetry.Exporter {
	if exp, ok := ctx.Value(keyExporter).(captelemetry.Exporter); ok && exp != nil {
		return exp
	}
	return Noop
}

// WithTurnID stores the active turn id for slog and exporter correlation.
func WithTurnID(ctx context.Context, turnID string) context.Context {
	if turnID == "" {
		return ctx
	}
	return context.WithValue(ctx, keyTurnID, turnID)
}

// TurnIDFrom returns the active turn id when present.
func TurnIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(keyTurnID).(string)
	return id
}

// WithToolParent marks the observation that should parent nested tool spans.
func WithToolParent(ctx context.Context, observationID string) context.Context {
	if observationID == "" {
		return ctx
	}
	return context.WithValue(ctx, keyToolParent, observationID)
}

// ToolParentFrom returns the active tool parent observation id when present.
func ToolParentFrom(ctx context.Context) string {
	id, _ := ctx.Value(keyToolParent).(string)
	return id
}

// WithParentObservationID is an alias for WithToolParent.
func WithParentObservationID(ctx context.Context, observationID string) context.Context {
	return WithToolParent(ctx, observationID)
}

// ParentObservationIDFrom is an alias for ToolParentFrom.
func ParentObservationIDFrom(ctx context.Context) string {
	return ToolParentFrom(ctx)
}

// WithScopeParent marks the span that should parent nested generations.
func WithScopeParent(ctx context.Context, observationID string) context.Context {
	if observationID == "" {
		return ctx
	}
	return context.WithValue(ctx, keyScopeParent, observationID)
}

// ScopeParentFrom returns the active scope parent observation id when present.
func ScopeParentFrom(ctx context.Context) string {
	id, _ := ctx.Value(keyScopeParent).(string)
	return id
}

// BeginTurn starts a turn on the exporter stored in ctx.
func BeginTurn(ctx context.Context, meta captelemetry.TurnMeta) (context.Context, func(captelemetry.TurnEnd)) {
	return ExporterFrom(ctx).BeginTurn(ctx, meta)
}

// BeginObservation starts a nested observation on the exporter stored in ctx.
func BeginObservation(ctx context.Context, meta captelemetry.ObservationMeta) (context.Context, func(captelemetry.ObservationEnd)) {
	return ExporterFrom(ctx).BeginObservation(ctx, meta)
}

// RecordEvent records a point-in-time event on the active trace.
func RecordEvent(ctx context.Context, name string, attrs map[string]string) {
	ExporterFrom(ctx).RecordEvent(ctx, name, EnrichEventAttrs(ctx, attrs))
}
