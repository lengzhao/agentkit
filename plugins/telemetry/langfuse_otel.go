package telemetry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/henomis/langfuse-go/model"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Langfuse v4 OpenTelemetry attribute keys, see
// https://langfuse.com/docs/opentelemetry for the full mapping.
const (
	otelAttrEnvironment            = "langfuse.environment"
	otelAttrRelease                = "langfuse.release"
	otelAttrSessionID              = "langfuse.session.id"
	otelAttrUserID                 = "langfuse.user.id"
	otelAttrTraceMetadataPrefix    = "langfuse.trace.metadata."
	otelAttrObservationType        = "langfuse.observation.type"
	otelAttrObservationInput       = "langfuse.observation.input"
	otelAttrObservationOutput      = "langfuse.observation.output"
	otelAttrObservationLevel       = "langfuse.observation.level"
	otelAttrObservationStatusMsg   = "langfuse.observation.status_message"
	otelAttrObservationMetaPrefix  = "langfuse.observation.metadata."
	otelAttrObservationModelName   = "langfuse.observation.model.name"
	otelAttrObservationModelParams = "langfuse.observation.model.parameters"
	otelAttrObservationUsage       = "langfuse.observation.usage_details"
	otelAttrCompletionStartTime    = "langfuse.observation.completion_start_time"
)

// otelIngestClient is the Langfuse v4 ingestion backend: it translates
// traces/observations into OpenTelemetry spans exported via OTLP/HTTP to
// POST {baseURL}/api/public/otel/v1/traces with the
// `x-langfuse-ingestion-version: 4` header.
type otelIngestClient struct {
	tp          *sdktrace.TracerProvider
	tracer      trace.Tracer
	environment string
	release     string

	mu     sync.Mutex
	traces map[string]*otelTraceState       // turn/trace ID → root span
	obs    map[string]*otelObservationState // observation ID → span
}

type otelTraceState struct {
	span      trace.Span
	sessionID string
	userID    string
}

type otelObservationState struct {
	span trace.Span
	// traceID ties the observation to its turn so traceUpdate can reclaim it;
	// ended spans stay resolvable as parents for late siblings (tool spans run
	// after their generation ended).
	traceID string
}

func newOTelIngestClient(ctx context.Context, baseURL, publicKey, secretKey string, flushInterval time.Duration, environment, release string) (*otelIngestClient, error) {
	auth := base64.StdEncoding.EncodeToString([]byte(publicKey + ":" + secretKey))
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(baseURL + "/api/public/otel/v1/traces"),
		otlptracehttp.WithHeaders(map[string]string{
			"Authorization":                "Basic " + auth,
			"x-langfuse-ingestion-version": "4",
		}),
	}
	if strings.HasPrefix(baseURL, "http://") {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("telemetry/langfuse otel exporter: %w", err)
	}
	return newOTelIngestClientWithExporter(exporter, flushInterval, environment, release), nil
}

func newOTelIngestClientWithExporter(exporter sdktrace.SpanExporter, flushInterval time.Duration, environment, release string) *otelIngestClient {
	batchOpts := []sdktrace.BatchSpanProcessorOption{}
	if flushInterval > 0 {
		batchOpts = append(batchOpts, sdktrace.WithBatchTimeout(flushInterval))
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithIDGenerator(otelIDGenerator{}),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "agentkit"))),
		sdktrace.WithBatcher(exporter, batchOpts...),
	)
	return &otelIngestClient{
		tp:          tp,
		tracer:      tp.Tracer("agentkit/telemetry/langfuse"),
		environment: environment,
		release:     release,
		traces:      map[string]*otelTraceState{},
		obs:         map[string]*otelObservationState{},
	}
}

// Trace handles both trace-create (first call for an ID) and trace-update
// (second call ends the root span), mirroring the exporter's two-phase usage.
func (c *otelIngestClient) Trace(t *model.Trace) error {
	c.mu.Lock()
	_, exists := c.traces[t.ID]
	c.mu.Unlock()
	if exists {
		return c.traceUpdate(t)
	}
	return c.traceCreate(t)
}

func (c *otelIngestClient) traceCreate(t *model.Trace) error {
	state := &otelTraceState{sessionID: t.SessionID, userID: t.UserID}
	ctx := context.WithValue(context.Background(), forcedTraceIDKey{}, otelTraceIDFromString(t.ID))
	attrs := c.baseAttrs(state)
	attrs = append(attrs, otelMetadataAttrs(otelAttrTraceMetadataPrefix, t.Metadata)...)
	if s := otelPayloadString(t.Input); s != "" {
		attrs = append(attrs, attribute.String(otelAttrObservationInput, s))
	}
	name := t.Name
	if name == "" {
		name = "agent.turn"
	}
	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	if t.Timestamp != nil {
		opts = append(opts, trace.WithTimestamp(*t.Timestamp))
	}
	_, state.span = c.tracer.Start(ctx, name, opts...)
	c.mu.Lock()
	c.traces[t.ID] = state
	c.mu.Unlock()
	return nil
}

func (c *otelIngestClient) traceUpdate(t *model.Trace) error {
	c.mu.Lock()
	state := c.traces[t.ID]
	delete(c.traces, t.ID)
	// The turn is over: reclaim its observation spans (kept until now so late
	// children can still resolve ended parents).
	for id, obs := range c.obs {
		if obs.traceID == t.ID {
			delete(c.obs, id)
		}
	}
	c.mu.Unlock()
	if state == nil || state.span == nil {
		return nil
	}
	attrs := otelMetadataAttrs(otelAttrTraceMetadataPrefix, t.Metadata)
	if s := otelPayloadString(t.Output); s != "" {
		attrs = append(attrs, attribute.String(otelAttrObservationOutput, s))
	}
	if len(attrs) > 0 {
		state.span.SetAttributes(attrs...)
	}
	state.span.End()
	return nil
}

func (c *otelIngestClient) Generation(g *model.Generation, parentObservationID *string) error {
	ctx, traceState := c.parentContext(g.TraceID, parentObservationID)
	attrs := c.baseAttrs(traceState)
	attrs = append(attrs, attribute.String(otelAttrObservationType, "generation"))
	if g.Model != "" {
		attrs = append(attrs, attribute.String(otelAttrObservationModelName, g.Model))
	}
	if s := otelPayloadString(g.Input); s != "" {
		attrs = append(attrs, attribute.String(otelAttrObservationInput, s))
	}
	if g.ModelParameters != nil {
		if s := otelPayloadString(g.ModelParameters); s != "" {
			attrs = append(attrs, attribute.String(otelAttrObservationModelParams, s))
		}
	}
	attrs = append(attrs, otelMetadataAttrs(otelAttrObservationMetaPrefix, g.Metadata)...)
	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	if g.StartTime != nil {
		opts = append(opts, trace.WithTimestamp(*g.StartTime))
	}
	_, span := c.tracer.Start(ctx, observationName(g.Name, "generation"), opts...)
	c.mu.Lock()
	c.obs[g.ID] = &otelObservationState{span: span, traceID: g.TraceID}
	c.mu.Unlock()
	return nil
}

func (c *otelIngestClient) GenerationEnd(g *model.Generation) error {
	span := c.observationSpan(g.ID)
	if span == nil {
		return nil
	}
	attrs := otelMetadataAttrs(otelAttrObservationMetaPrefix, g.Metadata)
	if s := otelPayloadString(g.Output); s != "" {
		attrs = append(attrs, attribute.String(otelAttrObservationOutput, s))
	}
	if g.Model != "" {
		attrs = append(attrs, attribute.String(otelAttrObservationModelName, g.Model))
	}
	if g.CompletionStartTime != nil {
		attrs = append(attrs, attribute.String(otelAttrCompletionStartTime, g.CompletionStartTime.UTC().Format(time.RFC3339Nano)))
	}
	if usage := otelUsageDetails(g.Usage); usage != "" {
		attrs = append(attrs, attribute.String(otelAttrObservationUsage, usage))
	}
	endSpan(span, g.Level, g.StatusMessage, g.EndTime, attrs)
	return nil
}

func (c *otelIngestClient) Span(s *model.Span, parentObservationID *string) error {
	ctx, traceState := c.parentContext(s.TraceID, parentObservationID)
	attrs := c.baseAttrs(traceState)
	attrs = append(attrs, attribute.String(otelAttrObservationType, "span"))
	if v := otelPayloadString(s.Input); v != "" {
		attrs = append(attrs, attribute.String(otelAttrObservationInput, v))
	}
	attrs = append(attrs, otelMetadataAttrs(otelAttrObservationMetaPrefix, s.Metadata)...)
	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	if s.StartTime != nil {
		opts = append(opts, trace.WithTimestamp(*s.StartTime))
	}
	_, span := c.tracer.Start(ctx, observationName(s.Name, "span"), opts...)
	c.mu.Lock()
	c.obs[s.ID] = &otelObservationState{span: span, traceID: s.TraceID}
	c.mu.Unlock()
	return nil
}

func (c *otelIngestClient) SpanEnd(s *model.Span) error {
	span := c.observationSpan(s.ID)
	if span == nil {
		return nil
	}
	attrs := otelMetadataAttrs(otelAttrObservationMetaPrefix, s.Metadata)
	if v := otelPayloadString(s.Output); v != "" {
		attrs = append(attrs, attribute.String(otelAttrObservationOutput, v))
	}
	endSpan(span, s.Level, s.StatusMessage, s.EndTime, attrs)
	return nil
}

// Event maps to a zero-duration OTel span with observation type "event".
func (c *otelIngestClient) Event(e *model.Event, parentObservationID *string) error {
	ctx, traceState := c.parentContext(e.TraceID, parentObservationID)
	attrs := c.baseAttrs(traceState)
	attrs = append(attrs, attribute.String(otelAttrObservationType, "event"))
	attrs = append(attrs, otelMetadataAttrs(otelAttrObservationMetaPrefix, e.Metadata)...)
	at := time.Now()
	if e.StartTime != nil {
		at = *e.StartTime
	}
	_, span := c.tracer.Start(ctx, observationName(e.Name, "event"),
		trace.WithAttributes(attrs...),
		trace.WithTimestamp(at),
	)
	span.End(trace.WithTimestamp(at))
	return nil
}

func (c *otelIngestClient) Flush(ctx context.Context) error {
	return c.tp.ForceFlush(ctx)
}

func (c *otelIngestClient) Shutdown(ctx context.Context) error {
	return c.tp.Shutdown(ctx)
}

// parentContext resolves the parent span for a new observation: the explicit
// parent observation when given, otherwise the turn root span, otherwise a
// fresh root with the turn-derived trace ID.
func (c *otelIngestClient) parentContext(traceID string, parentObservationID *string) (context.Context, *otelTraceState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	traceState := c.traces[traceID]
	if parentObservationID != nil {
		if obs := c.obs[*parentObservationID]; obs != nil {
			return trace.ContextWithSpan(context.Background(), obs.span), traceState
		}
	}
	if traceState != nil && traceState.span != nil {
		return trace.ContextWithSpan(context.Background(), traceState.span), traceState
	}
	ctx := context.WithValue(context.Background(), forcedTraceIDKey{}, otelTraceIDFromString(traceID))
	return ctx, traceState
}

// observationSpan resolves an observation span by ID. Spans are retained
// after End (reclaimed at traceUpdate) because OTel parenting only needs the
// parent SpanContext, which stays valid on ended spans — e.g. tool spans are
// started right after their generation span ended.
func (c *otelIngestClient) observationSpan(id string) trace.Span {
	c.mu.Lock()
	defer c.mu.Unlock()
	obs := c.obs[id]
	if obs == nil {
		return nil
	}
	return obs.span
}

// baseAttrs returns attributes shared by every span of a trace so the Langfuse
// v4 data model picks up session/user/environment on each observation.
func (c *otelIngestClient) baseAttrs(traceState *otelTraceState) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if c.environment != "" {
		attrs = append(attrs, attribute.String(otelAttrEnvironment, c.environment))
	}
	if c.release != "" {
		attrs = append(attrs, attribute.String(otelAttrRelease, c.release))
	}
	if traceState != nil {
		if traceState.sessionID != "" {
			attrs = append(attrs, attribute.String(otelAttrSessionID, traceState.sessionID))
		}
		if traceState.userID != "" {
			attrs = append(attrs, attribute.String(otelAttrUserID, traceState.userID))
		}
	}
	return attrs
}

func endSpan(span trace.Span, level model.ObservationLevel, statusMessage string, endTime *time.Time, attrs []attribute.KeyValue) {
	if level == model.ObservationLevelError {
		attrs = append(attrs, attribute.String(otelAttrObservationLevel, string(model.ObservationLevelError)))
		if statusMessage != "" {
			attrs = append(attrs, attribute.String(otelAttrObservationStatusMsg, statusMessage))
		}
	}
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	if level == model.ObservationLevelError {
		span.SetStatus(codes.Error, statusMessage)
	}
	opts := []trace.SpanEndOption{}
	if endTime != nil {
		opts = append(opts, trace.WithTimestamp(*endTime))
	}
	span.End(opts...)
}

// otelTraceIDFromString derives a stable OTel trace ID from the turn ID: UUIDs
// map directly onto the 16 bytes, anything else is hashed.
func otelTraceIDFromString(id string) trace.TraceID {
	if u, err := uuid.Parse(id); err == nil {
		return trace.TraceID(u)
	}
	sum := sha256.Sum256([]byte(id))
	var tid trace.TraceID
	copy(tid[:], sum[:16])
	return tid
}

func otelPayloadString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

func otelMetadataAttrs(prefix string, metadata any) []attribute.KeyValue {
	m, ok := metadata.(model.M)
	if !ok || len(m) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(m))
	for k, v := range m {
		if s := otelPayloadString(v); s != "" {
			attrs = append(attrs, attribute.String(prefix+k, s))
		}
	}
	return attrs
}

func otelUsageDetails(usage model.Usage) string {
	if usage.Input == 0 && usage.Output == 0 && usage.Total == 0 {
		return ""
	}
	details := map[string]int{
		"input":  usage.Input,
		"output": usage.Output,
		"total":  usage.Total,
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return ""
	}
	return string(raw)
}

func observationName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// forcedTraceIDKey carries the desired OTel trace ID for root spans so a
// Langfuse trace keeps the turn-derived ID across restarts of the context.
type forcedTraceIDKey struct{}

// otelIDGenerator honors forcedTraceIDKey for root spans and falls back to
// random IDs otherwise.
type otelIDGenerator struct{}

func (otelIDGenerator) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	if tid, ok := ctx.Value(forcedTraceIDKey{}).(trace.TraceID); ok && tid.IsValid() {
		return tid, newOTelSpanID()
	}
	return newOTelTraceID(), newOTelSpanID()
}

func (otelIDGenerator) NewSpanID(_ context.Context, _ trace.TraceID) trace.SpanID {
	return newOTelSpanID()
}

func newOTelTraceID() trace.TraceID {
	var tid trace.TraceID
	_, _ = rand.Read(tid[:])
	return tid
}

func newOTelSpanID() trace.SpanID {
	var sid trace.SpanID
	_, _ = rand.Read(sid[:])
	return sid
}
