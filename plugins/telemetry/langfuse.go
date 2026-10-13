package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/henomis/langfuse-go/model"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/pluginkit"
)

// Langfuse ingestion API versions.
const (
	// IngestVersionV3 is the legacy batch ingestion API (POST /api/public/ingestion).
	IngestVersionV3 = "v3"
	// IngestVersionV4 is the OpenTelemetry-based API (OTLP POST /api/public/otel/v1/traces).
	IngestVersionV4 = "v4"
)

type LangfuseConfig struct {
	// BaseURL is the Langfuse host, e.g. https://cloud.langfuse.com.
	BaseURL string `json:"baseUrl"`
	// PublicKeyRef resolves the Langfuse public key, e.g. env:LANGFUSE_PUBLIC_KEY.
	PublicKeyRef string `json:"publicKeyRef"`
	// SecretKeyRef resolves the Langfuse secret key, e.g. env:LANGFUSE_SECRET_KEY.
	SecretKeyRef string `json:"secretKeyRef"`
	// IngestVersion selects the Langfuse ingestion API: "v4" (default, OTLP
	// /api/public/otel/v1/traces, for Langfuse server v4) or "v3" (legacy batch
	// /api/public/ingestion, for Langfuse server v3 and earlier).
	IngestVersion string `json:"ingestVersion"`
	// Environment labels traces in Langfuse.
	Environment string `json:"environment"`
	// Release labels the deploying artifact.
	Release string `json:"release"`
	// SampleRate in (0,1]; 1 exports every turn.
	SampleRate float64 `json:"sampleRate"`
	// FlushIntervalSeconds configures the Langfuse SDK batch flush interval.
	FlushIntervalSeconds int `json:"flushIntervalSeconds"`
	// MaxPayloadBytes truncates exported input/output payloads. 0 means no limit.
	MaxPayloadBytes int `json:"maxPayloadBytes"`
	// MaxFieldBytes truncates individual message content fields in llm.generation
	// input while keeping JSON valid. Oversized fields end with
	// `\n...[truncated N]` where N is omitted bytes. Omit to default to 8192; set 0 for no limit.
	MaxFieldBytes *int `json:"maxFieldBytes"`
	// DeduplicateGenerationPrefix omits message prefixes identical to the previous
	// llm.generation observation within the same trace.
	DeduplicateGenerationPrefix *bool `json:"deduplicateGenerationPrefix"`
	// RedactInputs scrubs sensitive keys from exported inputs.
	RedactInputs bool `json:"redactInputs"`
	// RedactOutputs scrubs sensitive keys from exported outputs.
	RedactOutputs bool `json:"redactOutputs"`
}

// SetDefaults implements pluginkit.Defaulter.
func (c *LangfuseConfig) SetDefaults() {
	if strings.TrimSpace(c.BaseURL) == "" {
		c.BaseURL = "https://cloud.langfuse.com"
	}
	if strings.TrimSpace(c.IngestVersion) == "" {
		c.IngestVersion = IngestVersionV4
	}
	if c.SampleRate == 0 {
		c.SampleRate = 1
	}
	if c.FlushIntervalSeconds == 0 {
		c.FlushIntervalSeconds = 2
	}
}

// Validate implements pluginkit.Validator.
func (c *LangfuseConfig) Validate() error {
	switch strings.ToLower(strings.TrimSpace(c.IngestVersion)) {
	case IngestVersionV3, IngestVersionV4:
	default:
		return fmt.Errorf("telemetry/langfuse ingestVersion must be %q or %q, got %q", IngestVersionV3, IngestVersionV4, c.IngestVersion)
	}
	if c.SampleRate <= 0 || c.SampleRate > 1 {
		return fmt.Errorf("telemetry/langfuse sampleRate must be in (0, 1], got %v", c.SampleRate)
	}
	if c.FlushIntervalSeconds < 0 {
		return fmt.Errorf("telemetry/langfuse flushIntervalSeconds must not be negative")
	}
	if c.MaxPayloadBytes < 0 {
		return fmt.Errorf("telemetry/langfuse maxPayloadBytes must not be negative")
	}
	if c.MaxFieldBytes != nil && *c.MaxFieldBytes < 0 {
		return fmt.Errorf("telemetry/langfuse maxFieldBytes must not be negative")
	}
	return nil
}

type LangfuseDeps struct {
	Credentials credentials.Store    `json:"credentials"`
	Telemetry   captelemetry.Toolkit `json:"telemetry"`
}

// ingestClient abstracts the two Langfuse ingestion APIs (v3 batch ingestion,
// v4 OTLP) behind the event model the exporter already produces.
type ingestClient interface {
	Trace(t *model.Trace) error
	Generation(g *model.Generation, parentObservationID *string) error
	GenerationEnd(g *model.Generation) error
	Span(s *model.Span, parentObservationID *string) error
	SpanEnd(s *model.Span) error
	Event(e *model.Event, parentObservationID *string) error
	Flush(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type Langfuse struct {
	client                 ingestClient
	maxPayloadBytes        int
	maxFieldBytes          int
	dedupeGenerationPrefix bool
	genMessages            map[string][]agentkit.ModelMessage
	obsMetadata            map[string]map[string]map[string]string
	redactInputs           bool
	redactOutputs          bool
	sampleRate             float64
	environment            string
	release                string
	telemetry              captelemetry.Toolkit
	mu                     sync.Mutex
	rng                    *rand.Rand
}

type langfuseCtxKey int

const (
	langfuseKeyTraceID langfuseCtxKey = iota
)

func init() {
	pluginkit.Register("telemetry/langfuse", NewLangfuse)
}

// NewLangfuse registers telemetry/langfuse: Export traces to Langfuse via the Go SDK ingestion API.
//
// Best practices:
//   - Point baseUrl at your Langfuse host, e.g. https://cloud.langfuse.com.
//   - Keep keys in credentials/env via publicKeyRef and secretKeyRef.
//   - Pair with runner.default and loop.default telemetry deps so shutdown flushes spans.
func NewLangfuse(cfg LangfuseConfig, deps LangfuseDeps) (captelemetry.Exporter, error) {
	if deps.Credentials == nil {
		return nil, fmt.Errorf("telemetry/langfuse requires credentials dependency")
	}
	if deps.Telemetry == nil {
		return nil, fmt.Errorf("telemetry/langfuse requires telemetry dependency")
	}
	publicKey, err := resolveCredential(context.Background(), deps.Credentials, cfg.PublicKeyRef, "LANGFUSE_PUBLIC_KEY")
	if err != nil {
		return nil, err
	}
	secretKey, err := resolveCredential(context.Background(), deps.Credentials, cfg.SecretKeyRef, "LANGFUSE_SECRET_KEY")
	if err != nil {
		return nil, err
	}

	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	sampleRate := cfg.SampleRate
	flushInterval := time.Duration(cfg.FlushIntervalSeconds) * time.Second
	maxPayload := cfg.MaxPayloadBytes

	maxFieldBytes := 8192
	if cfg.MaxFieldBytes != nil {
		maxFieldBytes = *cfg.MaxFieldBytes
	}
	if maxFieldBytes < 0 {
		maxFieldBytes = 0
	}

	dedupePrefix := true
	if cfg.DeduplicateGenerationPrefix != nil {
		dedupePrefix = *cfg.DeduplicateGenerationPrefix
	}

	env := strings.TrimSpace(cfg.Environment)
	if env == "" {
		env = "default"
	}

	var client ingestClient
	switch strings.ToLower(strings.TrimSpace(cfg.IngestVersion)) {
	case IngestVersionV3:
		client = newIngestionAPIClient(context.Background(), baseURL, publicKey, secretKey, flushInterval)
	default:
		otelClient, err := newOTelIngestClient(context.Background(), baseURL, publicKey, secretKey, flushInterval, env, strings.TrimSpace(cfg.Release))
		if err != nil {
			return nil, err
		}
		client = otelClient
	}

	return &Langfuse{
		client:                 client,
		maxPayloadBytes:        maxPayload,
		maxFieldBytes:          maxFieldBytes,
		dedupeGenerationPrefix: dedupePrefix,
		genMessages:            map[string][]agentkit.ModelMessage{},
		redactInputs:           cfg.RedactInputs,
		redactOutputs:          cfg.RedactOutputs,
		sampleRate:             sampleRate,
		environment:            env,
		release:                strings.TrimSpace(cfg.Release),
		telemetry:              deps.Telemetry,
		rng:                    rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

func resolveCredential(ctx context.Context, store credentials.Store, ref, fallbackEnv string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "env:" + fallbackEnv
	}
	secret, err := store.Resolve(ctx, credentials.GlobalScope, ref)
	if err != nil {
		return "", fmt.Errorf("telemetry/langfuse credential %q: %w", ref, err)
	}
	value := strings.TrimSpace(secret.Value)
	if value == "" {
		return "", fmt.Errorf("telemetry/langfuse credential %q is empty", ref)
	}
	return value, nil
}

func (l *Langfuse) BeginTurn(ctx context.Context, meta captelemetry.TurnMeta) (context.Context, func(captelemetry.TurnEnd)) {
	if !l.shouldSample() {
		return ctx, func(captelemetry.TurnEnd) {}
	}
	traceID := meta.TurnID
	if traceID == "" {
		traceID = uuid.NewString()
	}
	ctx = l.telemetry.WithTurnID(ctx, traceID)
	ctx = context.WithValue(ctx, langfuseKeyTraceID, traceID)

	trace := &model.Trace{
		ID:        traceID,
		Name:      "agent.turn",
		SessionID: meta.SessionID,
		UserID:    meta.UserID,
		Input:     l.preparePayload(meta.Input, l.redactInputs),
		Release:   l.release,
		Metadata:  stringMapToM(l.traceMetadata(meta)),
	}
	if err := l.client.Trace(trace); err != nil {
		slog.Warn("telemetry/langfuse trace create failed", "trace_id", traceID, "err", err)
	}

	ctx = l.telemetry.WithExporter(ctx, l)
	l.clearGenerationState(traceID)
	return ctx, func(end captelemetry.TurnEnd) {
		l.clearGenerationState(traceID)
		update := &model.Trace{
			ID:     traceID,
			Output: l.preparePayload(end.Output, l.redactOutputs),
		}
		if md := l.turnEndMetadata(end); len(md) > 0 {
			merged := l.traceMetadata(meta)
			for k, v := range md {
				merged[k] = v
			}
			update.Metadata = stringMapToM(merged)
		}
		if err := l.client.Trace(update); err != nil {
			slog.Warn("telemetry/langfuse trace update failed", "trace_id", traceID, "err", err)
		}
	}
}

func (l *Langfuse) BeginObservation(ctx context.Context, meta captelemetry.ObservationMeta) (context.Context, func(captelemetry.ObservationEnd)) {
	if !l.shouldSample() {
		return ctx, func(captelemetry.ObservationEnd) {}
	}
	traceID, _ := ctx.Value(langfuseKeyTraceID).(string)
	if traceID == "" {
		return ctx, func(captelemetry.ObservationEnd) {}
	}

	name := meta.Name
	if name == "" {
		name = string(meta.Kind)
	}
	now := time.Now().UTC()
	obsMetadata := l.buildObservationMetadata(ctx, meta)

	switch meta.Kind {
	case captelemetry.KindGeneration:
		gen := &model.Generation{
			TraceID:   traceID,
			Name:      name,
			Model:     meta.Model,
			Input:     l.prepareGenerationInput(traceID, meta),
			StartTime: &now,
			Metadata:  stringMapToM(obsMetadata),
		}
		if params := l.generationModelParameters(meta); params != nil {
			gen.ModelParameters = params
		}
		gen.ID = uuid.NewString()
		if err := l.client.Generation(gen, l.scopeParentPtr(ctx)); err != nil {
			slog.Warn("telemetry/langfuse generation create failed", "trace_id", traceID, "name", name, "err", err)
			return ctx, func(captelemetry.ObservationEnd) {}
		}
		l.storeObservationMetadata(traceID, gen.ID, obsMetadata)
		plannedModel := meta.Model
		ctx = l.telemetry.WithToolParent(ctx, gen.ID)
		return ctx, func(end captelemetry.ObservationEnd) {
			endTime := time.Now().UTC()
			md := l.observationMetadataByID(traceID, gen.ID, obsMetadata)
			planned := strings.TrimSpace(plannedModel)
			if planned == "" {
				planned = strings.TrimSpace(md["model"])
			}
			actual := strings.TrimSpace(end.ActualModel)
			if actual == "" {
				actual = planned
			}
			if actual != "" {
				md["model"] = actual
			}
			if planned != "" && actual != "" && planned != actual {
				md["model_planned"] = planned
			}
			update := &model.Generation{
				ID:       gen.ID,
				TraceID:  traceID,
				Output:   l.preparePayload(end.Output, l.redactOutputs),
				EndTime:  &endTime,
				Metadata: stringMapToM(md),
			}
			if actual != "" {
				update.Model = actual
			}
			if !end.CompletionStartTime.IsZero() {
				completionStart := end.CompletionStartTime.UTC()
				if !end.FirstTextTime.IsZero() {
					completionStart = end.FirstTextTime.UTC()
				}
				update.CompletionStartTime = &completionStart
			}
			if end.Usage != nil {
				update.Usage = langfuseUsage(end.Usage)
			}
			if end.Err != nil {
				update.Level = model.ObservationLevelError
				update.StatusMessage = end.Err.Error()
			}
			if err := l.client.GenerationEnd(update); err != nil {
				slog.Warn("telemetry/langfuse generation end failed", "trace_id", traceID, "observation_id", gen.ID, "err", err)
			}
		}
	default:
		span := &model.Span{
			TraceID:   traceID,
			Name:      name,
			Input:     l.preparePayload(meta.Input, l.redactInputs),
			StartTime: &now,
			Metadata:  stringMapToM(obsMetadata),
		}
		span.ID = uuid.NewString()
		if err := l.client.Span(span, l.toolParentPtr(ctx)); err != nil {
			slog.Warn("telemetry/langfuse span create failed", "trace_id", traceID, "name", name, "err", err)
			return ctx, func(captelemetry.ObservationEnd) {}
		}
		l.storeObservationMetadata(traceID, span.ID, obsMetadata)
		ctx = l.telemetry.WithToolParent(ctx, span.ID)
		if meta.Scope {
			ctx = l.telemetry.WithScopeParent(ctx, span.ID)
		}
		return ctx, func(end captelemetry.ObservationEnd) {
			endTime := time.Now().UTC()
			update := &model.Span{
				ID:       span.ID,
				TraceID:  traceID,
				Output:   l.preparePayload(end.Output, l.redactOutputs),
				EndTime:  &endTime,
				Metadata: stringMapToM(l.observationMetadataByID(traceID, span.ID, obsMetadata)),
			}
			if end.Err != nil {
				update.Level = model.ObservationLevelError
				update.StatusMessage = end.Err.Error()
			}
			if err := l.client.SpanEnd(update); err != nil {
				slog.Warn("telemetry/langfuse span end failed", "trace_id", traceID, "observation_id", span.ID, "err", err)
			}
		}
	}
}

func (l *Langfuse) RecordEvent(ctx context.Context, name string, attrs map[string]string) {
	if !l.shouldSample() {
		return
	}
	traceID, _ := ctx.Value(langfuseKeyTraceID).(string)
	if traceID == "" {
		return
	}
	now := time.Now().UTC()
	event := &model.Event{
		TraceID:   traceID,
		Name:      name,
		StartTime: &now,
		Metadata:  stringMapToM(l.telemetry.EnrichEventAttrs(ctx, attrs)),
	}
	if err := l.client.Event(event, l.scopeParentPtr(ctx)); err != nil {
		slog.Warn("telemetry/langfuse event failed", "trace_id", traceID, "name", name, "err", err)
	}
}

func (l *Langfuse) Flush(ctx context.Context) error {
	return l.client.Flush(ctx)
}

func (l *Langfuse) Shutdown(ctx context.Context) error {
	return l.client.Shutdown(ctx)
}

func (l *Langfuse) shouldSample() bool {
	if l.sampleRate >= 1 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rng.Float64() <= l.sampleRate
}

func (l *Langfuse) preparePayload(value string, redact bool) any {
	if value == "" {
		return nil
	}
	return l.telemetry.PreparePayload(value, l.maxPayloadBytes, redact)
}

func (l *Langfuse) prepareGenerationInput(traceID string, meta captelemetry.ObservationMeta) any {
	messages := meta.GenerationMessages
	if len(messages) == 0 && strings.TrimSpace(meta.Input) != "" {
		return l.preparePayload(meta.Input, l.redactInputs)
	}
	if len(messages) == 0 {
		return nil
	}

	l.mu.Lock()
	prev := l.genMessages[traceID]
	formatted := l.telemetry.FormatGenerationInputForExport(prev, messages, l.maxFieldBytes, l.dedupeGenerationPrefix)
	l.genMessages[traceID] = cloneMessages(messages)
	l.mu.Unlock()

	if l.redactInputs {
		formatted = l.telemetry.RedactJSON(formatted)
	}
	if formatted == "" {
		return nil
	}
	return formatted
}

func (l *Langfuse) clearGenerationState(traceID string) {
	l.mu.Lock()
	delete(l.genMessages, traceID)
	delete(l.obsMetadata, traceID)
	l.mu.Unlock()
}

func cloneMessages(messages []agentkit.ModelMessage) []agentkit.ModelMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]agentkit.ModelMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		if len(msg.Content) > 0 {
			out[i].Content = append([]agentkit.ContentPart(nil), msg.Content...)
		}
		if len(msg.ToolCalls) > 0 {
			out[i].ToolCalls = append([]agentkit.ToolCall(nil), msg.ToolCalls...)
		}
		if len(msg.ToolResults) > 0 {
			out[i].ToolResults = append([]agentkit.ToolResult(nil), msg.ToolResults...)
		}
	}
	return out
}

func (l *Langfuse) traceMetadata(meta captelemetry.TurnMeta) map[string]string {
	out := map[string]string{
		"environment": l.environment,
	}
	if meta.TurnID != "" {
		out["turn_id"] = meta.TurnID
	}
	if meta.DeliverySessionID != "" {
		out["delivery_session_id"] = meta.DeliverySessionID
	}
	if meta.AgentID != "" {
		out["agent_id"] = meta.AgentID
	}
	if meta.Model != "" {
		out["model"] = meta.Model
	}
	if meta.PlatformID != "" {
		out["platform_id"] = meta.PlatformID
	}
	if meta.UserID != "" {
		out["user_id"] = meta.UserID
	}
	if meta.WorkspaceKey != "" {
		out["workspace_key"] = meta.WorkspaceKey
	}
	if meta.AttachmentSources != "" {
		out["attachment_sources"] = meta.AttachmentSources
	}
	return out
}

func (l *Langfuse) buildObservationMetadata(ctx context.Context, meta captelemetry.ObservationMeta) map[string]string {
	out := l.telemetry.ContextObservationAttrs(ctx)
	if meta.AgentID != "" {
		out["agent_id"] = meta.AgentID
	}
	if meta.SessionID != "" {
		out["session_id"] = meta.SessionID
	}
	if meta.Model != "" {
		out["model"] = meta.Model
	}
	if meta.Kind != "" {
		out["observation_kind"] = string(meta.Kind)
	}
	if meta.Name != "" {
		out["observation_name"] = meta.Name
	}
	if len(meta.ToolNames) > 0 {
		out["tools"] = strings.Join(meta.ToolNames, ",")
	}
	for k, v := range meta.Attributes {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	if len(out) == 0 && meta.Name != "" {
		out["observation_name"] = meta.Name
	}
	return out
}

func stringMapToM(m map[string]string) model.M {
	if len(m) == 0 {
		return nil
	}
	out := make(model.M, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (l *Langfuse) storeObservationMetadata(traceID, obsID string, md map[string]string) {
	if traceID == "" || obsID == "" || len(md) == 0 {
		return
	}
	copied := make(map[string]string, len(md))
	for k, v := range md {
		copied[k] = v
	}
	l.mu.Lock()
	if l.obsMetadata == nil {
		l.obsMetadata = map[string]map[string]map[string]string{}
	}
	if l.obsMetadata[traceID] == nil {
		l.obsMetadata[traceID] = map[string]map[string]string{}
	}
	l.obsMetadata[traceID][obsID] = copied
	l.mu.Unlock()
}

func (l *Langfuse) observationMetadataByID(traceID, obsID string, fallback map[string]string) map[string]string {
	l.mu.Lock()
	traceMD := l.obsMetadata[traceID]
	var stored map[string]string
	if traceMD != nil {
		stored = traceMD[obsID]
	}
	l.mu.Unlock()
	if len(stored) > 0 {
		return stored
	}
	return fallback
}

func (l *Langfuse) generationModelParameters(meta captelemetry.ObservationMeta) map[string]any {
	if len(meta.ToolNames) == 0 {
		return nil
	}
	return map[string]any{"tools": meta.ToolNames}
}

func (l *Langfuse) turnEndMetadata(end captelemetry.TurnEnd) map[string]string {
	out := map[string]string{}
	if end.Steps > 0 {
		out["steps"] = fmt.Sprint(end.Steps)
	}
	if end.StopReason != "" {
		out["stop_reason"] = end.StopReason
	}
	if end.Usage != nil {
		out["usage_input_tokens"] = fmt.Sprint(end.Usage.InputTokens)
		out["usage_output_tokens"] = fmt.Sprint(end.Usage.OutputTokens)
		out["usage_total_tokens"] = fmt.Sprint(end.Usage.TotalTokens)
	}
	if end.Err != nil {
		out["error"] = end.Err.Error()
	}
	return out
}

func (l *Langfuse) toolParentPtr(ctx context.Context) *string {
	parentID := l.telemetry.ToolParentFrom(ctx)
	if parentID == "" {
		return nil
	}
	return &parentID
}

func (l *Langfuse) scopeParentPtr(ctx context.Context) *string {
	parentID := l.telemetry.ScopeParentFrom(ctx)
	if parentID == "" {
		return nil
	}
	return &parentID
}

func langfuseUsage(usage *captelemetry.Usage) model.Usage {
	return model.Usage{
		Input:            usage.InputTokens,
		Output:           usage.OutputTokens,
		Total:            usage.TotalTokens,
		Unit:             model.ModelUsageUnitTokens,
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.TotalTokens,
	}
}
