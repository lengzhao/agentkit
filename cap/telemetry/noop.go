package telemetry

import "context"

// Noop is the default exporter that discards all telemetry.
var Noop Exporter = noopExporter{}

type noopExporter struct{}

func (noopExporter) BeginTurn(ctx context.Context, _ TurnMeta) (context.Context, func(TurnEnd)) {
	return ctx, func(TurnEnd) {}
}

func (noopExporter) BeginObservation(ctx context.Context, _ ObservationMeta) (context.Context, func(ObservationEnd)) {
	return ctx, func(ObservationEnd) {}
}

func (noopExporter) RecordEvent(context.Context, string, map[string]string) {}

func (noopExporter) Flush(context.Context) error { return nil }

func (noopExporter) Shutdown(context.Context) error { return nil }
