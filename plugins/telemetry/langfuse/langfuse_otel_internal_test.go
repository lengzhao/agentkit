package langfuse

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/henomis/langfuse-go/model"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type collectingSpanExporter struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (e *collectingSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	e.spans = append(e.spans, spans...)
	e.mu.Unlock()
	return nil
}

func (e *collectingSpanExporter) Shutdown(context.Context) error { return nil }

func (e *collectingSpanExporter) byName(name string) sdktrace.ReadOnlySpan {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// 复现生产时序：agent.runStep 里 generation 的 endObservation 是 defer，runStep
// 返回后外层才执行 tool —— tool span Start 时其 parent generation 已经 End。
// OTel 下 parent 只需 SpanContext（ended span 仍有效），不能因为 generation End
// 就把 tool span 挂到根上。
func TestOTelToolSpanKeepsEndedGenerationParent(t *testing.T) {
	exporter := &collectingSpanExporter{}
	client := newOTelIngestClientWithExporter(exporter, 0, "test", "")

	turnID := uuid.NewString()
	if err := client.Trace(&model.Trace{ID: turnID, Name: "agent.turn"}); err != nil {
		t.Fatal(err)
	}
	genID := uuid.NewString()
	if err := client.Generation(&model.Generation{ID: genID, TraceID: turnID, Name: "llm.generation"}, nil); err != nil {
		t.Fatal(err)
	}
	// generation 先于 tool span 结束（与 runtime 一致）。
	if err := client.GenerationEnd(&model.Generation{ID: genID, TraceID: turnID}); err != nil {
		t.Fatal(err)
	}
	toolID := uuid.NewString()
	if err := client.Span(&model.Span{ID: toolID, TraceID: turnID, Name: "tool.bash"}, &genID); err != nil {
		t.Fatal(err)
	}
	if err := client.SpanEnd(&model.Span{ID: toolID, TraceID: turnID}); err != nil {
		t.Fatal(err)
	}
	if err := client.Trace(&model.Trace{ID: turnID}); err != nil {
		t.Fatal(err)
	}
	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	root := exporter.byName("agent.turn")
	gen := exporter.byName("llm.generation")
	tool := exporter.byName("tool.bash")
	if root == nil || gen == nil || tool == nil {
		t.Fatalf("missing spans: root=%v gen=%v tool=%v", root != nil, gen != nil, tool != nil)
	}
	if got, want := tool.Parent().SpanID(), gen.SpanContext().SpanID(); got != want {
		t.Fatalf("tool span parent = %s, want generation span %s", got, want)
	}
	if got, want := gen.Parent().SpanID(), root.SpanContext().SpanID(); got != want {
		t.Fatalf("generation parent = %s, want root span %s", got, want)
	}
	parsed, err := uuid.Parse(turnID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := root.SpanContext().TraceID(), trace.TraceID(parsed); got != want {
		t.Fatalf("root trace ID = %s, want turn-derived %s", got, want)
	}
}

// trace update 后应回收该 trace 的 observation spans，避免泄漏。
func TestOTelTraceUpdateReclaimsObservations(t *testing.T) {
	exporter := &collectingSpanExporter{}
	client := newOTelIngestClientWithExporter(exporter, 0, "test", "")

	turnID := uuid.NewString()
	if err := client.Trace(&model.Trace{ID: turnID, Name: "agent.turn"}); err != nil {
		t.Fatal(err)
	}
	genID := uuid.NewString()
	if err := client.Generation(&model.Generation{ID: genID, TraceID: turnID, Name: "llm.generation"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.GenerationEnd(&model.Generation{ID: genID, TraceID: turnID}); err != nil {
		t.Fatal(err)
	}
	if err := client.Trace(&model.Trace{ID: turnID}); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	remaining := len(client.obs)
	client.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("obs map leaked %d entries after trace update", remaining)
	}
	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
