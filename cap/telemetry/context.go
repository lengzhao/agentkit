package telemetry

import "context"

type ctxKey int

const (
	keyExporter ctxKey = iota
	keyTurnID
)

// WithExporter stores an exporter on the context for downstream runtime code.
func WithExporter(ctx context.Context, exp Exporter) context.Context {
	if exp == nil {
		return ctx
	}
	return context.WithValue(ctx, keyExporter, exp)
}

// ExporterFrom returns the exporter on ctx, or Noop when unset.
func ExporterFrom(ctx context.Context) Exporter {
	if exp, ok := ctx.Value(keyExporter).(Exporter); ok && exp != nil {
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

// BeginTurn starts a turn on the exporter stored in ctx.
func BeginTurn(ctx context.Context, meta TurnMeta) (context.Context, func(TurnEnd)) {
	return ExporterFrom(ctx).BeginTurn(ctx, meta)
}

// BeginObservation starts a nested observation on the exporter stored in ctx.
func BeginObservation(ctx context.Context, meta ObservationMeta) (context.Context, func(ObservationEnd)) {
	return ExporterFrom(ctx).BeginObservation(ctx, meta)
}

// RecordEvent records a point-in-time event on the active trace.
func RecordEvent(ctx context.Context, name string, attrs map[string]string) {
	ExporterFrom(ctx).RecordEvent(ctx, name, attrs)
}
