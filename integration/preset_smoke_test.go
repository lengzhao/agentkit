//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/agentkit/testing/presettest"
)

func TestIntegrationSubagentSmokePreset(t *testing.T) {
	if testing.Short() {
		t.Skip("integration preset run")
	}

	result := presettest.RunOnce(t, "调研一下 loop 串行机制", "presets/subagent-smoke.yaml")
	ctx := context.Background()
	events := agenttest.SessionEvents(t, ctx, result.Store, result.SessionID)
	agenttest.AssertSubagentParentSession(t, events)
}

func TestIntegrationAutonomousSmokePreset(t *testing.T) {
	if testing.Short() {
		t.Skip("integration preset run")
	}

	result := presettest.RunOnce(t, "读取 README 并汇报", "presets/autonomous-smoke.yaml")
	ctx := context.Background()
	events := agenttest.SessionEvents(t, ctx, result.Store, result.SessionID)

	if got := agenttest.CountEvents(events, agentkit.EventSessionRecovery); got != 0 {
		t.Fatalf("session/recovery = %d, want 0", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventRunFinish); got < 1 {
		t.Fatalf("run/finish = %d, want at least 1", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTodoUpdate); got < 2 {
		t.Fatalf("todo/update = %d, want at least 2", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnContinue); got < 1 {
		t.Fatalf("turn/continue = %d, want hook-driven continuation", got)
	}
}

func TestIntegrationAutonomousWorkerSmokePreset(t *testing.T) {
	if testing.Short() {
		t.Skip("integration preset run")
	}

	result := presettest.RunOnce(t, "读取 README 并汇报",
		"presets/autonomous-smoke.yaml",
		"presets/worker.yaml",
	)
	ctx := context.Background()
	events := agenttest.SessionEvents(t, ctx, result.Store, result.SessionID)

	if got := agenttest.CountEvents(events, agentkit.EventSessionRecovery); got != 0 {
		t.Fatalf("session/recovery = %d, want 0", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventRunFinish); got < 1 {
		t.Fatalf("run/finish = %d, want at least 1", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTodoUpdate); got < 2 {
		t.Fatalf("todo/update = %d, want at least 2", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnContinue); got < 1 {
		t.Fatalf("turn/continue = %d, want hook-driven continuation", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got < 1 {
		t.Fatalf("turn/end = %d, want at least 1", got)
	}
}

func TestIntegrationCodingSmokePreset(t *testing.T) {
	if testing.Short() {
		t.Skip("integration preset run")
	}

	result := presettest.RunOnce(t, "列出目录并读取 README", "presets/coding-smoke.yaml")
	ctx := context.Background()
	events := agenttest.SessionEvents(t, ctx, result.Store, result.SessionID)

	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got < 1 {
		t.Fatalf("turn/end = %d, want at least 1", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventToolResult); got < 2 {
		t.Fatalf("tool/result = %d, want bash + read results", got)
	}
}
