//go:build integration

package integration_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/hook"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/agentkit/testing/presettest"
)

// E2E-405: /status output matches session event aggregates after an autonomous preset run.
func TestIntegrationStatusMatchesSessionEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("integration status")
	}

	const maxContinuations = 3
	result := presettest.RunOnce(t, "读取 README 并汇报", "presets/autonomous-smoke.yaml")
	ctx := agenttest.TurnContext(result.SessionID, agentkit.AgentID("smoke"))
	events := agenttest.SessionEvents(t, ctx, result.Store, result.SessionID)

	startSeq := session.RunStartSeq(events)
	usage := session.TotalUsage(events, startSeq)
	todos := session.LatestTodos(events)
	pending := session.PendingTodos(todos)
	finish := session.FinishAfter(events, startSeq)

	provider, err := hook.NewTurnContinue(
		hook.TurnContinueConfig{MaxContinuations: maxContinuations},
		hook.TurnContinueDeps{SessionStore: result.Store},
	)
	if err != nil {
		t.Fatal(err)
	}
	commands, ok := provider.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("hook/turn-continue should contribute commands")
	}
	var status agentkit.Command
	for _, cmd := range commands.Commands() {
		if cmd.Name() == "status" {
			status = cmd
			break
		}
	}
	if status == nil {
		t.Fatal("no /status command")
	}

	out, err := status.CommandExec(ctx, "")
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	wantTokens := fmt.Sprintf("tokens this run: %d (in %d / out %d)",
		usage.TotalTokens, usage.InputTokens, usage.OutputTokens)
	for _, want := range []string{
		fmt.Sprintf("max continuations: %d", maxContinuations),
		wantTokens,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}

	if finish == nil {
		t.Fatal("expected run/finish in session events")
	}
	wantFinish := fmt.Sprintf("finished: %s — %s", finish.Status, finish.Summary)
	if !strings.Contains(out, wantFinish) {
		t.Fatalf("status output missing %q:\n%s", wantFinish, out)
	}

	if len(todos) == 0 {
		t.Fatal("expected todo/update events")
	}
	wantTasks := fmt.Sprintf("tasks: %d pending of %d", len(pending), len(todos))
	if !strings.Contains(out, wantTasks) {
		t.Fatalf("status output missing %q:\n%s", wantTasks, out)
	}
	for _, item := range todos {
		wantLine := fmt.Sprintf("[%s] %s (id: %s)", item.Status, item.Title, item.ID)
		if !strings.Contains(out, wantLine) {
			t.Fatalf("status output missing task line %q:\n%s", wantLine, out)
		}
	}
}
