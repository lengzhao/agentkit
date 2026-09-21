package telemetry

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// Toolkit is the standard ctx-scoped telemetry helper surface (implementation: runtime/telemetry).
type Toolkit interface {
	WithExporter(context.Context, Exporter) context.Context
	ExporterFrom(context.Context) Exporter
	WithTurnID(context.Context, string) context.Context
	WithToolParent(context.Context, string) context.Context
	WithScopeParent(context.Context, string) context.Context
	ToolParentFrom(context.Context) string
	ScopeParentFrom(context.Context) string
	BeginTurn(context.Context, TurnMeta) (context.Context, func(TurnEnd))
	BeginObservation(context.Context, ObservationMeta) (context.Context, func(ObservationEnd))
	RecordEvent(context.Context, string, map[string]string)
	ObservationMetaFromContext(context.Context, ObservationMeta) ObservationMeta
	ContextObservationAttrs(context.Context) map[string]string
	EnrichEventAttrs(context.Context, map[string]string) map[string]string
	FormatMessage(agentkit.ModelMessage) string
	FormatGenerationInputForExport(prev, cur []agentkit.ModelMessage, maxFieldBytes int, dedupePrefix bool) string
	RedactJSON(string) string
	PreparePayload(raw string, maxBytes int, redact bool) string
	AttachmentSpanAttrs(attachments []agentkit.InboundAttachment) map[string]string
}
