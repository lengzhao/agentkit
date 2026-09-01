package compaction_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/cap/telemetry"
)

type countingCompaction struct {
	applied bool
}

func (c *countingCompaction) Compact(_ context.Context, req compaction.Request) (compaction.Result, error) {
	if req.Force {
		c.applied = true
		return compaction.Result{Applied: true, Messages: req.Messages}, nil
	}
	return compaction.Result{Messages: req.Messages}, nil
}

func TestApplyAllRecordsCompactionSpan(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	ctx := telemetry.WithExporter(context.Background(), rec)
	ctx, _ = rec.BeginTurn(ctx, telemetry.TurnMeta{TurnID: "turn-1"})

	svc := &countingCompaction{}
	_, applied, err := compaction.ApplyAll(ctx, []compaction.Service{svc}, compaction.Request{
		Messages: []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}}},
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
		t.Fatalf("observations = %d, want 1", len(observations))
	}
	obs := observations[0]
	if obs.Meta.Name != "compaction.apply" {
		t.Fatalf("name = %q", obs.Meta.Name)
	}
	if obs.Meta.Input != "force" {
		t.Fatalf("input = %q, want force", obs.Meta.Input)
	}
	if obs.End.Output != "applied 1 service(s)" {
		t.Fatalf("output = %q", obs.End.Output)
	}
}

func TestApplyAllSkipsCompactionSpanWhenNoop(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	ctx := telemetry.WithExporter(context.Background(), rec)
	ctx, _ = rec.BeginTurn(ctx, telemetry.TurnMeta{TurnID: "turn-1"})

	svc := &countingCompaction{}
	_, applied, err := compaction.ApplyAll(ctx, []compaction.Service{svc}, compaction.Request{
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
