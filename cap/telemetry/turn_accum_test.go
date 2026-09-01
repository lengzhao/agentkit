package telemetry_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/cap/telemetry"
)

func TestTurnEndFromAccumIncludesStepsAndStopReason(t *testing.T) {
	t.Parallel()

	ctx := telemetry.WithTurnAccum(context.Background())
	telemetry.RecordTurnSteps(ctx, 3)
	telemetry.RecordTurnStopReason(ctx, "complete")
	telemetry.RecordTurnUsage(ctx, telemetry.Usage{
		InputTokens:  10,
		OutputTokens: 5,
		TotalTokens:  15,
	})

	end := telemetry.TurnEndFromAccum(ctx)
	if end.Steps != 3 {
		t.Fatalf("steps = %d, want 3", end.Steps)
	}
	if end.StopReason != "complete" {
		t.Fatalf("stop_reason = %q", end.StopReason)
	}
	if end.Usage == nil || end.Usage.TotalTokens != 15 {
		t.Fatalf("usage = %#v", end.Usage)
	}
}
