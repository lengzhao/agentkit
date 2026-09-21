package telemetry

import (
	"context"

	"github.com/lengzhao/agentkit"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
)

type standardToolkit struct{}

func (standardToolkit) WithExporter(ctx context.Context, exp captelemetry.Exporter) context.Context {
	return WithExporter(ctx, exp)
}

func (standardToolkit) ExporterFrom(ctx context.Context) captelemetry.Exporter {
	return ExporterFrom(ctx)
}

func (standardToolkit) WithTurnID(ctx context.Context, turnID string) context.Context {
	return WithTurnID(ctx, turnID)
}

func (standardToolkit) WithToolParent(ctx context.Context, observationID string) context.Context {
	return WithToolParent(ctx, observationID)
}

func (standardToolkit) WithScopeParent(ctx context.Context, observationID string) context.Context {
	return WithScopeParent(ctx, observationID)
}

func (standardToolkit) ToolParentFrom(ctx context.Context) string {
	return ToolParentFrom(ctx)
}

func (standardToolkit) ScopeParentFrom(ctx context.Context) string {
	return ScopeParentFrom(ctx)
}

func (standardToolkit) BeginTurn(ctx context.Context, meta captelemetry.TurnMeta) (context.Context, func(captelemetry.TurnEnd)) {
	return BeginTurn(ctx, meta)
}

func (standardToolkit) BeginObservation(ctx context.Context, meta captelemetry.ObservationMeta) (context.Context, func(captelemetry.ObservationEnd)) {
	return BeginObservation(ctx, meta)
}

func (standardToolkit) RecordEvent(ctx context.Context, name string, attrs map[string]string) {
	RecordEvent(ctx, name, attrs)
}

func (standardToolkit) ObservationMetaFromContext(ctx context.Context, meta captelemetry.ObservationMeta) captelemetry.ObservationMeta {
	return ObservationMetaFromContext(ctx, meta)
}

func (standardToolkit) ContextObservationAttrs(ctx context.Context) map[string]string {
	return ContextObservationAttrs(ctx)
}

func (standardToolkit) EnrichEventAttrs(ctx context.Context, attrs map[string]string) map[string]string {
	return EnrichEventAttrs(ctx, attrs)
}

func (standardToolkit) FormatMessage(msg agentkit.ModelMessage) string {
	return FormatMessage(msg)
}

func (standardToolkit) FormatGenerationInputForExport(prev, cur []agentkit.ModelMessage, maxFieldBytes int, dedupePrefix bool) string {
	return FormatGenerationInputForExport(prev, cur, maxFieldBytes, dedupePrefix)
}

func (standardToolkit) RedactJSON(raw string) string {
	return RedactJSON(raw)
}

func (standardToolkit) PreparePayload(raw string, maxBytes int, redact bool) string {
	return PreparePayload(raw, maxBytes, redact)
}

func (standardToolkit) AttachmentSpanAttrs(attachments []agentkit.InboundAttachment) map[string]string {
	return AttachmentSpanAttrs(attachments)
}

func NewToolkit(_ struct{}, _ struct{}) (captelemetry.Toolkit, error) {
	return standardToolkit{}, nil
}
