package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/pluginkit"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
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
	// FlushIntervalSeconds configures the OTel batch processor interval.
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
	tracer          trace.Tracer
	provider        *sdktrace.TracerProvider
	maxPayloadBytes int
	redactInputs    bool
	redactOutputs   bool
	sampleRate      float64
	environment     string
	release         string
	mu              sync.Mutex
	rng             *rand.Rand
}

func init() {
	pluginkit.Register("telemetry/langfuse", NewLangfuse)
}

// NewLangfuse registers telemetry/langfuse: Export traces to Langfuse via OTLP/HTTP.
//
// Best practices:
//   - Point baseUrl at your Langfuse region, e.g. https://cloud.langfuse.com.
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
	endpoint := baseURL + "/api/public/otel"

	auth := base64.StdEncoding.EncodeToString([]byte(publicKey + ":" + secretKey))
	headers := map[string]string{
		"Authorization":               "Basic " + auth,
		"x-langfuse-ingestion-version": "4",
	}

	client := otlptracehttp.NewClient(
		otlptracehttp.WithEndpointURL(endpoint),
		otlptracehttp.WithHeaders(headers),
	)
	exporter, err := otlptrace.New(context.Background(), client)
	if err != nil {
		return nil, fmt.Errorf("telemetry/langfuse otlp exporter: %w", err)
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

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "agentkit"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry/langfuse resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(flushInterval)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)

	return &Langfuse{
		tracer:          provider.Tracer("github.com/lengzhao/agentkit/telemetry/langfuse"),
		provider:        provider,
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
	turnID := meta.TurnID
	if turnID == "" {
		turnID = uuid.NewString()
	}
	ctx = captelemetry.WithTurnID(ctx, turnID)

	name := "agent.turn"
	attrs := l.baseTraceAttrs(meta)
	input := captelemetry.PreparePayload(meta.Input, l.maxPayloadBytes, l.redactInputs)
	if input != "" {
		attrs = append(attrs, attribute.String("langfuse.observation.input", input))
	}

	ctx, span := l.tracer.Start(ctx, name, trace.WithNewRoot(), trace.WithAttributes(attrs...))
	ctx = captelemetry.WithExporter(ctx, l)
	return ctx, func(end captelemetry.TurnEnd) {
		if end.Output != "" {
			output := captelemetry.PreparePayload(end.Output, l.maxPayloadBytes, l.redactOutputs)
			span.SetAttributes(attribute.String("langfuse.observation.output", output))
		}
		if end.Err != nil {
			span.RecordError(end.Err)
			span.SetStatus(codes.Error, end.Err.Error())
		}
		span.End()
	}
}

func (l *Langfuse) BeginObservation(ctx context.Context, meta captelemetry.ObservationMeta) (context.Context, func(captelemetry.ObservationEnd)) {
	if !l.shouldSample() {
		return ctx, func(captelemetry.ObservationEnd) {}
	}
	name := meta.Name
	if name == "" {
		name = string(meta.Kind)
	}
	obsType := observationType(meta.Kind)
	attrs := []attribute.KeyValue{
		attribute.String("langfuse.observation.type", obsType),
	}
	if meta.Model != "" {
		attrs = append(attrs, attribute.String("langfuse.observation.model.name", meta.Model))
	}
	if meta.Input != "" {
		input := captelemetry.PreparePayload(meta.Input, l.maxPayloadBytes, l.redactInputs)
		attrs = append(attrs, attribute.String("langfuse.observation.input", input))
	}

	ctx, span := l.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, func(end captelemetry.ObservationEnd) {
		if end.Output != "" {
			output := captelemetry.PreparePayload(end.Output, l.maxPayloadBytes, l.redactOutputs)
			span.SetAttributes(attribute.String("langfuse.observation.output", output))
		}
		if end.Usage != nil {
			usage := map[string]int{
				"input":  end.Usage.InputTokens,
				"output": end.Usage.OutputTokens,
				"total":  end.Usage.TotalTokens,
			}
			if raw, err := json.Marshal(usage); err == nil {
				span.SetAttributes(attribute.String("langfuse.observation.usage_details", string(raw)))
			}
		}
		if end.Err != nil {
			span.RecordError(end.Err)
			span.SetStatus(codes.Error, end.Err.Error())
		}
		span.End()
	}
}

func (l *Langfuse) RecordEvent(ctx context.Context, name string, attrs map[string]string) {
	if !l.shouldSample() {
		return
	}
	otelAttrs := make([]attribute.KeyValue, 0, len(attrs)+2)
	otelAttrs = append(otelAttrs,
		attribute.String("langfuse.observation.type", "event"),
		attribute.String("langfuse.event.name", name),
	)
	for k, v := range attrs {
		otelAttrs = append(otelAttrs, attribute.String("langfuse.event."+k, v))
	}
	_, span := l.tracer.Start(ctx, name, trace.WithAttributes(otelAttrs...))
	span.End()
}

func (l *Langfuse) Flush(ctx context.Context) error {
	if l.provider == nil {
		return nil
	}
	return l.provider.ForceFlush(ctx)
}

func (l *Langfuse) Shutdown(ctx context.Context) error {
	if l.provider == nil {
		return nil
	}
	if err := l.provider.ForceFlush(ctx); err != nil {
		slog.Warn("telemetry/langfuse flush failed", "err", err)
	}
	return l.provider.Shutdown(ctx)
}

func (l *Langfuse) shouldSample() bool {
	if l.sampleRate >= 1 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rng.Float64() <= l.sampleRate
}

func (l *Langfuse) baseTraceAttrs(meta captelemetry.TurnMeta) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("langfuse.trace.name", "agent.turn"),
		attribute.String("langfuse.environment", l.environment),
	}
	if l.release != "" {
		attrs = append(attrs, attribute.String("langfuse.release", l.release))
	}
	if meta.SessionID != "" {
		attrs = append(attrs, attribute.String("langfuse.session.id", meta.SessionID))
		attrs = append(attrs, attribute.String("session.id", meta.SessionID))
	}
	if meta.DeliverySessionID != "" {
		attrs = append(attrs, attribute.String("agentkit.delivery_session_id", meta.DeliverySessionID))
	}
	if meta.AgentID != "" {
		attrs = append(attrs, attribute.String("agentkit.agent_id", meta.AgentID))
	}
	if meta.PlatformID != "" {
		attrs = append(attrs, attribute.String("agentkit.platform_id", meta.PlatformID))
	}
	if meta.UserID != "" {
		attrs = append(attrs, attribute.String("langfuse.user.id", meta.UserID))
		attrs = append(attrs, attribute.String("user.id", meta.UserID))
	}
	if meta.TurnID != "" {
		attrs = append(attrs, attribute.String("agentkit.turn_id", meta.TurnID))
	}
	return attrs
}

func observationType(kind captelemetry.ObservationKind) string {
	switch kind {
	case captelemetry.KindGeneration:
		return "generation"
	case captelemetry.KindTool:
		return "tool"
	default:
		return "span"
	}
}
