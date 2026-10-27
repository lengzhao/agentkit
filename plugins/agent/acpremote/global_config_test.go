package acpremote

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/acpclient"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestACPGlobalConfig(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("existing", "claude"), 10*time.Second)
	defer cancel()
	run := func(ctx context.Context, args, want string) string {
		t.Helper()
		out, err := cmd.CommandExec(ctx, args)
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("%s = %s, %v; want %q", args, out, err, want)
		}
		return out
	}
	run(ctx, "model", "current: sonnet")
	run(ctx, "model -g opus", "global default: opus")
	run(ctx, "mode -g plan", "global default: plan")
	run(ctx, "effort -g high", "global default: high")
	run(ctx, "config -g fast-mode true", "global default: true")
	run(ctx, "model", "current: sonnet")
	run(ctx, "effort", "current: low")
	run(context.Background(), "model -g", "global default: opus")
	next := agenttest.TurnContext("next", "claude")
	run(next, "model", "current: opus")
	run(next, "mode", "current: plan")
	run(next, "effort", "current: high")
	run(next, "config fast-mode", "current: true")
	run(next, "model sonnet", "current: sonnet")
	run(context.Background(), "model --global", "global default: opus")
	if _, err := cmd.CommandExec(ctx, "effort -g rejected"); err == nil {
		t.Fatal("rejected global value was persisted")
	}
	run(context.Background(), "effort -g", "global default: high")
	// A fresh runtime over the same filesystem reads persisted defaults.
	ag, err := New(a.cfg, Deps{Workspace: a.workspace, FS: a.fs, Telemetry: a.telemetry})
	if err != nil {
		t.Fatal(err)
	}
	reloaded := ag.(*Runtime)
	t.Cleanup(func() {
		for _, hook := range reloaded.ShutdownHooks() {
			hook()
		}
	})
	out, err := reloaded.Commands()[0].CommandExec(agenttest.TurnContext("reloaded", "claude"), "model")
	if err != nil || !strings.Contains(out, "current: opus") {
		t.Fatalf("persisted default = %s, %v", out, err)
	}
	// Another agent sharing storage must not inherit Claude's defaults.
	cfg := a.cfg
	cfg.ID = "codex"
	other, err := New(cfg, Deps{Workspace: a.workspace, FS: a.fs, Telemetry: a.telemetry})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, hook := range other.(*Runtime).ShutdownHooks() {
			hook()
		}
	})
	out, err = other.(agentkit.CommandProvider).Commands()[0].CommandExec(context.Background(), "model -g")
	if err != nil || !strings.Contains(out, "(not set)") {
		t.Fatalf("agent default leaked: %s, %v", out, err)
	}
}

func TestACPGlobalReadDoesNotConnect(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	out, err := cmd.CommandExec(context.Background(), "model -g")
	if err != nil || !strings.Contains(out, "(not set)") {
		t.Fatalf("global read = %s, %v", out, err)
	}
	if a.bridgeCount() != 0 {
		t.Fatal("global read initialized a bridge")
	}
	for _, args := range []string{"model opus -g", "model -g -g opus", "-g model opus", "config effort -g high"} {
		if _, err := cmd.CommandExec(context.Background(), args); err == nil {
			t.Fatalf("misplaced flag accepted: %q", args)
		}
	}
}

func TestACPGlobalDefaultsDoNotOverrideRestoredSession(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	a.sessionRestore = acpclient.NewRestorer(acpclient.StrategyResume)
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("restored", "claude"), 10*time.Second)
	defer cancel()
	if _, err := cmd.CommandExec(ctx, "model"); err != nil {
		t.Fatal(err)
	}
	if _, err := cmd.CommandExec(ctx, "model -g opus"); err != nil {
		t.Fatal(err)
	}
	a.bridgeFor("restored").releaseSubprocess()
	out, err := cmd.CommandExec(ctx, "model")
	if err != nil || !strings.Contains(out, "current: sonnet") {
		t.Fatalf("restore was overridden: %s, %v", out, err)
	}
}

func TestACPGlobalDefaultsApplyModelBeforeEffort(t *testing.T) {
	_, cmd := configTestRuntime(t, "claude", "supported")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, args := range []string{"model -g opus", "effort -g low"} {
		if _, err := cmd.CommandExec(ctx, args); err != nil {
			t.Fatalf("%s: %v", args, err)
		}
	}
	out, err := cmd.CommandExec(agenttest.TurnContext("ordered", "claude"), "effort")
	if err != nil || !strings.Contains(out, "current: low") {
		t.Fatalf("model overwrote global effort: %s, %v", out, err)
	}
}

func TestACPGlobalConfigCanReplaceStaleDefault(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := cmd.CommandExec(ctx, "model -g opus"); err != nil {
		t.Fatal(err)
	}
	store := &globalConfigStore{fs: a.fs, agentID: a.id}
	defaults, err := store.load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a model that was removed by a newer remote agent version.
	option := defaults["llm"]
	selected := *option.Select
	selected.CurrentValue = "removed-model"
	option.Select = &selected
	defaults["llm"] = option
	if err := store.save(ctx, defaults); err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CommandExec(ctx, "model -g sonnet")
	if err != nil || !strings.Contains(out, "global default: sonnet") {
		t.Fatalf("cannot repair stale default: %s, %v", out, err)
	}
}
