package compaction_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

type countingCompaction struct {
	applied bool
}

func (c *countingCompaction) Compact(_ context.Context, req capscompaction.Request) (capscompaction.Result, error) {
	if req.Force {
		c.applied = true
		return capscompaction.Result{Applied: true, Messages: req.Messages}, nil
	}
	return capscompaction.Result{Messages: req.Messages}, nil
}

func TestApplyAllRecordsCompactionSpan(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	ctx := telemetry.WithExporter(context.Background(), rec)
	ctx, _ = rec.BeginTurn(ctx, captelemetry.TurnMeta{TurnID: "turn-1"})

	svc := &countingCompaction{}
	_, applied, err := rtcompaction.ApplyAll(ctx, []capscompaction.Service{svc}, capscompaction.Request{
		SessionID: "sess-1",
		Messages:  []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}}},
		Force:     true,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}

	_, observations, _ := rec.Snapshot()
	if len(observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(observations))
	}
	obs := observations[0]
	if obs.Meta.Name != "compaction.apply" {
		t.Fatalf("name = %q", obs.Meta.Name)
	}
	if obs.Meta.Input != "force" {
		t.Fatalf("input = %q, want force", obs.Meta.Input)
	}
	if obs.Meta.Attributes["mode"] != "force" {
		t.Fatalf("mode attr = %q", obs.Meta.Attributes["mode"])
	}
	if obs.Meta.Attributes["tokens_before"] == "" || obs.Meta.Attributes["tokens_after"] == "" {
		t.Fatalf("missing token attrs: %#v", obs.Meta.Attributes)
	}
	if obs.Meta.Attributes["messages_before"] != "1" || obs.Meta.Attributes["messages_after"] != "1" {
		t.Fatalf("message attrs = %#v", obs.Meta.Attributes)
	}
	if obs.End.Output == "applied 1 service(s)" || !containsAll(obs.End.Output, "applied=1", "tokens_before=", "tokens_after=") {
		t.Fatalf("output = %q, want size metrics", obs.End.Output)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestApplyAllSkipsCompactionSpanWhenNoop(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	ctx := telemetry.WithExporter(context.Background(), rec)
	ctx, _ = rec.BeginTurn(ctx, captelemetry.TurnMeta{TurnID: "turn-1"})

	svc := &countingCompaction{}
	_, applied, err := rtcompaction.ApplyAll(ctx, []capscompaction.Service{svc}, capscompaction.Request{
		Messages: []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied != 0 {
		t.Fatalf("applied = %d, want 0", applied)
	}

	_, observations, _ := rec.Snapshot()
	if len(observations) != 0 {
		t.Fatalf("observations = %d, want 0 when compaction is a no-op", len(observations))
	}
}

type wrappingCompaction struct {
	inner []capscompaction.Service
}

func (w *wrappingCompaction) Compact(ctx context.Context, req capscompaction.Request) (capscompaction.Result, error) {
	messages, applied, err := rtcompaction.ApplyAll(ctx, w.inner, req)
	if err != nil {
		return capscompaction.Result{}, err
	}
	return capscompaction.Result{Applied: applied > 0, Messages: messages}, nil
}

// pipeline → token-limit → summary 会嵌套三次 ApplyAll；Langfuse 上应只有最外层一条 span。
func TestApplyAllRecordsSingleSpanForNestedCalls(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	ctx := telemetry.WithExporter(context.Background(), rec)
	ctx, _ = rec.BeginTurn(ctx, captelemetry.TurnMeta{TurnID: "turn-nested"})

	leaf := &countingCompaction{}
	mid := &wrappingCompaction{inner: []capscompaction.Service{leaf}}
	outer := &wrappingCompaction{inner: []capscompaction.Service{mid}}
	_, applied, err := rtcompaction.ApplyAll(ctx, []capscompaction.Service{outer}, capscompaction.Request{
		Messages: []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello world"}}}},
		Force:    true,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	_, observations, _ := rec.Snapshot()
	if len(observations) != 1 {
		t.Fatalf("observations = %d, want 1 outermost span (not 3 nested ApplyAll)", len(observations))
	}
}
