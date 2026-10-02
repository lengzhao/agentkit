//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/agentkit/testing/presettest"
)

// E2E-041: platform/worker runs multiple prompt tasks sequentially (one turn/end each).
func TestIntegrationWorkerThreePromptTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("integration worker multi-task")
	}

	result := presettest.RunWorker(t, []string{
		"worker task one",
		"worker task two",
		"worker task three",
	}, "presets/worker-three-turn-smoke.yaml")

	ctx := context.Background()
	events := agenttest.SessionEvents(t, ctx, result.Store, result.SessionID)

	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got != 3 {
		t.Fatalf("turn/end = %d, want 3", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventUserMessage); got != 3 {
		t.Fatalf("user messages = %d, want 3", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventSessionRecovery); got != 0 {
		t.Fatalf("session/recovery = %d, want 0", got)
	}
}
