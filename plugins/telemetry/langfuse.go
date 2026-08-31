package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/henomis/langfuse-go"
	"github.com/henomis/langfuse-go/model"
	"github.com/lengzhao/pluginkit"
)

type LangfuseConfig struct {
	// BaseURL is the Langfuse host, e.g. https://cloud.langfuse.com.
	BaseURL string `json:"baseUrl"`
	// PublicKeyRef resolves the Langfuse public key, e.g. env:LANGFUSE_PUBLIC_KEY.
	PublicKeyRef string `json:"publicKeyRef"`
	// SecretKeyRef resolves the Langfuse secret key, e.g. env:LANGFUSE_SECRET_KEY.
	SecretKeyRef string `json:"secretKeyRef"`
	// Environment labels traces in Langfuse.
	Environment string `json:"environment"`
	// Release labels the deploying artifact.
	Release string `json:"release"`
	// SampleRate in (0,1]; 1 exports every turn.
	SampleRate float64 `json:"sampleRate"`
	// FlushIntervalSeconds configures the Langfuse SDK batch flush interval.
	FlushIntervalSeconds int `json:"flushIntervalSeconds"`
	// MaxPayloadBytes truncates exported input/output payloads.
	MaxPayloadBytes int `json:"maxPayloadBytes"`
	// RedactInputs scrubs sensitive keys from exported inputs.
	RedactInputs bool `json:"redactInputs"`
	// RedactOutputs scrubs sensitive keys from exported outputs.
	RedactOutputs bool `json:"redactOutputs"`
}

type LangfuseDeps struct {
	Credentials credentials.Store `json:"credentials"`
}

type Langfuse struct {
	client          *langfuse.Langfuse
	maxPayloadBytes int
	redactInputs    bool
	redactOutputs   bool
	sampleRate      float64
	environment     string
	release         string
	mu              sync.Mutex
	rng             *rand.Rand
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
	publicKey, err := resolveCredential(context.Background(), deps.Credentials, cfg.PublicKeyRef, "LANGFUSE_PUBLIC_KEY")
	if err != nil {
		return nil, err
	}
	secretKey, err := resolveCredential(context.Background(), deps.Credentials, cfg.SecretKeyRef, "LANGFUSE_SECRET_KEY")
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://cloud.langfuse.com"
	}

	sampleRate := cfg.SampleRate
	if sampleRate <= 0 {
		sampleRate = 1
	}
	if sampleRate > 1 {
		sampleRate = 1
	}

	flushInterval := time.Duration(cfg.FlushIntervalSeconds) * time.Second
	if flushInterval <= 0 {
		flushInterval = 2 * time.Second
	}

	maxPayload := cfg.MaxPayloadBytes
	if maxPayload <= 0 {
		maxPayload = 8192
	}

	env := strings.TrimSpace(cfg.Environment)
	if env == "" {
		env = "default"
	}

	// henomis/langfuse-go reads host and keys from process env at client construction.
	os.Setenv("LANGFUSE_HOST", baseURL)
	os.Setenv("LANGFUSE_PUBLIC_KEY", publicKey)
	os.Setenv("LANGFUSE_SECRET_KEY", secretKey)

	client := langfuse.New(context.Background()).WithFlushInterval(flushInterval)

	return &Langfuse{
		client:          client,
		maxPayloadBytes: maxPayload,
		redactInputs:    cfg.RedactInputs,
		redactOutputs:   cfg.RedactOutputs,
		sampleRate:      sampleRate,
		environment:     env,
		release:         strings.TrimSpace(cfg.Release),
		rng:             rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

func resolveCredential(ctx context.Context, store credentials.Store, ref, fallbackEnv string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "env:" + fallbackEnv
	}
	secret, err := store.Resolve(ctx, ref)
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
	ctx = captelemetry.WithTurnID(ctx, traceID)
	ctx = context.WithValue(ctx, langfuseKeyTraceID, traceID)

	trace := &model.Trace{
		ID:        traceID,
		Name:      "agent.turn",
		SessionID: meta.SessionID,
		UserID:    meta.UserID,
		Input:     l.preparePayload(meta.Input, l.redactInputs),
		Release:   l.release,
		Metadata:  l.traceMetadata(meta),
	}
	if _, err := l.client.Trace(trace); err != nil {
		slog.Warn("telemetry/langfuse trace create failed", "trace_id", traceID, "err", err)
	}

	ctx = captelemetry.WithExporter(ctx, l)
	return ctx, func(end captelemetry.TurnEnd) {
		update := &model.Trace{
			ID:     traceID,
			Output: l.preparePayload(end.Output, l.redactOutputs),
		}
		if md := l.turnEndMetadata(end); len(md) > 0 {
			update.Metadata = md
		}
		if _, err := l.client.Trace(update); err != nil {
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
	obsMetadata := l.observationMetadata(meta)

	switch meta.Kind {
	case captelemetry.KindGeneration:
		gen := &model.Generation{
			TraceID:   traceID,
			Name:      name,
			Model:     meta.Model,
			Input:     l.preparePayload(meta.Input, l.redactInputs),
			StartTime: &now,
			Metadata:  obsMetadata,
		}
		if params := l.generationModelParameters(meta); params != nil {
			gen.ModelParameters = params
		}
		created, err := l.client.Generation(gen, l.scopeParentPtr(ctx))
		if err != nil {
			slog.Warn("telemetry/langfuse generation create failed", "trace_id", traceID, "name", name, "err", err)
			return ctx, func(captelemetry.ObservationEnd) {}
		}
		ctx = captelemetry.WithToolParent(ctx, created.ID)
		return ctx, func(end captelemetry.ObservationEnd) {
			endTime := time.Now().UTC()
			update := &model.Generation{
				ID:      created.ID,
				TraceID: traceID,
				Output:  l.preparePayload(end.Output, l.redactOutputs),
				EndTime: &endTime,
			}
			if end.Usage != nil {
				update.Usage = langfuseUsage(end.Usage)
			}
			if end.Err != nil {
				update.Level = model.ObservationLevelError
				update.StatusMessage = end.Err.Error()
			}
			if _, err := l.client.GenerationEnd(update); err != nil {
				slog.Warn("telemetry/langfuse generation end failed", "trace_id", traceID, "observation_id", created.ID, "err", err)
			}
		}
	default:
		span := &model.Span{
			TraceID:   traceID,
			Name:      name,
			Input:     l.preparePayload(meta.Input, l.redactInputs),
			StartTime: &now,
			Metadata:  obsMetadata,
		}
		created, err := l.client.Span(span, l.toolParentPtr(ctx))
		if err != nil {
			slog.Warn("telemetry/langfuse span create failed", "trace_id", traceID, "name", name, "err", err)
			return ctx, func(captelemetry.ObservationEnd) {}
		}
		ctx = captelemetry.WithToolParent(ctx, created.ID)
		if meta.Scope {
			ctx = captelemetry.WithScopeParent(ctx, created.ID)
		}
		return ctx, func(end captelemetry.ObservationEnd) {
			endTime := time.Now().UTC()
			update := &model.Span{
				ID:      created.ID,
				TraceID: traceID,
				Output:  l.preparePayload(end.Output, l.redactOutputs),
				EndTime: &endTime,
			}
			if end.Err != nil {
				update.Level = model.ObservationLevelError
				update.StatusMessage = end.Err.Error()
			}
			if _, err := l.client.SpanEnd(update); err != nil {
				slog.Warn("telemetry/langfuse span end failed", "trace_id", traceID, "observation_id", created.ID, "err", err)
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
		Metadata:  captelemetry.EnrichEventAttrs(ctx, attrs),
	}
	if _, err := l.client.Event(event, l.scopeParentPtr(ctx)); err != nil {
		slog.Warn("telemetry/langfuse event failed", "trace_id", traceID, "name", name, "err", err)
	}
}

func (l *Langfuse) Flush(ctx context.Context) error {
	l.client.Flush(ctx)
	return nil
}

func (l *Langfuse) Shutdown(ctx context.Context) error {
	l.client.Flush(ctx)
	return nil
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
	return captelemetry.PreparePayload(value, l.maxPayloadBytes, redact)
}

func (l *Langfuse) traceMetadata(meta captelemetry.TurnMeta) map[string]string {
	out := map[string]string{
		"environment": l.environment,
	}
	if meta.DeliverySessionID != "" {
		out["delivery_session_id"] = meta.DeliverySessionID
	}
	if meta.AgentID != "" {
		out["agent_id"] = meta.AgentID
	}
	if meta.PlatformID != "" {
		out["platform_id"] = meta.PlatformID
	}
	if meta.TurnID != "" {
		out["turn_id"] = meta.TurnID
	}
	return out
}

func (l *Langfuse) observationMetadata(meta captelemetry.ObservationMeta) map[string]string {
	out := map[string]string{}
	if meta.AgentID != "" {
		out["agent_id"] = meta.AgentID
	}
	if meta.SessionID != "" {
		out["session_id"] = meta.SessionID
	}
	if len(meta.ToolNames) > 0 {
		out["tools"] = strings.Join(meta.ToolNames, ",")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (l *Langfuse) generationModelParameters(meta captelemetry.ObservationMeta) map[string]any {
	if len(meta.ToolNames) == 0 {
		return nil
	}
	return map[string]any{"tools": meta.ToolNames}
}

func (l *Langfuse) turnEndMetadata(end captelemetry.TurnEnd) map[string]string {
	out := map[string]string{}
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
	parentID := captelemetry.ToolParentFrom(ctx)
	if parentID == "" {
		return nil
	}
	return &parentID
}

func (l *Langfuse) scopeParentPtr(ctx context.Context) *string {
	parentID := captelemetry.ScopeParentFrom(ctx)
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
