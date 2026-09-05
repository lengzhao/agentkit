package telemetry

import (
	"context"

	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
)

// Noop is the default exporter that discards all telemetry.
var Noop captelemetry.Exporter = noopExporter{}

type noopExporter struct{}

func (noopExporter) BeginTurn(ctx context.Context, _ captelemetry.TurnMeta) (context.Context, func(captelemetry.TurnEnd)) {
	return ctx, func(captelemetry.TurnEnd) {}
}

func (noopExporter) BeginObservation(ctx context.Context, _ captelemetry.ObservationMeta) (context.Context, func(captelemetry.ObservationEnd)) {
	return ctx, func(captelemetry.ObservationEnd) {}
}

func (noopExporter) RecordEvent(context.Context, string, map[string]string) {}

func (noopExporter) Flush(context.Context) error { return nil }

func (noopExporter) Shutdown(context.Context) error { return nil }
