package smoke_test_test

import (
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/hook"
	"github.com/lengzhao/agentkit/plugins/tool/finish"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	todotool "github.com/lengzhao/agentkit/plugins/tool/todo"
	"github.com/lengzhao/agentkit/runtime/agent"
	hookruntime "github.com/lengzhao/agentkit/runtime/hooks"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type autonomousOpts struct {
	maxSteps int
	budget   *agent.BudgetConfig
	hookCfg  hook.TurnContinueConfig
	steps    []llm.ScriptedStep
}

func buildAutonomousAgent(t *testing.T, opts autonomousOpts) (agentkit.Agent, agentkit.SessionStore) {
	t.Helper()

	store, _ := agenttest.TempFileStore(t)

	tcHook, err := hook.NewTurnContinue(opts.hookCfg, hook.TurnContinueDeps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	hooksRT, err := hookruntime.New(hookruntime.Config{}, hookruntime.Deps{
		Providers: []agentkit.HookProvider{tcHook},
	})
	if err != nil {
		t.Fatal(err)
	}

	readPack, err := fs.NewFSMemory(fs.FSMemoryConfig{
		Files: map[string]string{"README.md": "hello", "same.go": "package main"},
		Tools: []string{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	todoTool, err := todotool.NewTodo(todotool.TodoConfig{}, todotool.TodoDeps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	finishTool, err := finish.NewFinish(finish.FinishConfig{}, finish.FinishDeps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		ToolPacks: []agentkit.ToolPack{readPack},
		Tools:     []agentkit.Tool{todoTool, finishTool},
		Approval:  agenttest.AllowAll{},
	})
	if err != nil {
		t.Fatal(err)
	}

	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: opts.steps})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}

	maxSteps := opts.maxSteps
	if maxSteps <= 0 {
		maxSteps = 5
	}
	ag, err := agent.New(agent.Config{
		ID:       "smoke",
		MaxSteps: maxSteps,
		Budget:   opts.budget,
	}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRT,
		Prompt:       assembler,
		Hooks:        hooksRT,
		Workspace:    agenttest.TestWorkspace(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return ag, store
}

func turnContinueReasons(t *testing.T, events []agentkit.SessionEvent) []string {
	t.Helper()
	var reasons []string
	for _, ev := range events {
		if ev.Type != agentkit.EventTurnContinue {
			continue
		}
		var data session.TurnContinueData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			t.Fatalf("decode turn/continue: %v", err)
		}
		reasons = append(reasons, data.Reason)
	}
	return reasons
}

// E2E-020: a segment that hits maxSteps still continues when hook/turn-continue sees pending work.
func TestSmokeAutonomousStepLimitContinuation(t *testing.T) {
	t.Parallel()

	ag, store := buildAutonomousAgent(t, autonomousOpts{
		maxSteps: 1,
		budget:   &agent.BudgetConfig{MaxContinuations: 3},
		hookCfg:  hook.TurnContinueConfig{MaxContinuations: 3, StallLimit: 10},
		steps: []llm.ScriptedStep{
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-todo", Name: "todo",
				Input: []byte(`{"op":"set","items":[{"id":"1","title":"read README","status":"pending"}]}`),
			}}},
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-read", Name: "read", Input: []byte(`{"path":"README.md"}`),
			}}},
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-done", Name: "todo", Input: []byte(`{"op":"complete","ids":["1"]}`),
			}}},
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-finish", Name: "finish",
				Input: []byte(`{"status":"completed","summary":"step-limit smoke done"}`),
			}}},
			{Text: "完成。"},
		},
	})

	sessionID := agentkit.SessionID("smoke:step-limit")
	ctx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, ctx, ag, "自主续跑冒烟")

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	if got := agenttest.CountEvents(events, agentkit.EventTurnContinue); got < 1 {
		t.Fatalf("turn/continue = %d, want at least 1", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventStepStart); got < 2 {
		t.Fatalf("step/start = %d, want multiple segments", got)
	}
	reasons := turnContinueReasons(t, events)
	foundStepLimit := false
	for _, reason := range reasons {
		if reason == string(agentkit.StopStepLimit) {
			foundStepLimit = true
			break
		}
	}
	if !foundStepLimit {
		t.Fatalf("turn/continue reasons = %v, want step-limit", reasons)
	}
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
}

// E2E-021: hard budget stops the run even when turn-continue would keep going.
func TestSmokeAutonomousBudgetExhaustionStopsRun(t *testing.T) {
	t.Parallel()

	ag, store := buildAutonomousAgent(t, autonomousOpts{
		maxSteps: 1,
		budget:   &agent.BudgetConfig{MaxTotalSteps: 2, MaxContinuations: 10},
		hookCfg:  hook.TurnContinueConfig{MaxContinuations: 10, StallLimit: 10},
		steps: []llm.ScriptedStep{
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-todo", Name: "todo",
				Input: []byte(`{"op":"set","items":[{"id":"1","title":"keep going","status":"pending"}]}`),
			}}},
			{Text: "segment one"},
			{Text: "segment two"},
			{Text: "segment three"},
		},
	})

	sessionID := agentkit.SessionID("smoke:budget")
	ctx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, ctx, ag, "预算耗尽冒烟")

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got != 1 {
		t.Fatalf("turn/end = %d, want 1", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventStepStart); got != 2 {
		t.Fatalf("step/start = %d, want 2 (maxTotalSteps)", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnContinue); got != 1 {
		t.Fatalf("turn/continue = %d, want exactly 1 before budget blocks further segments", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventRunFinish); got != 0 {
		t.Fatalf("run/finish = %d, want 0 when budget ends the run early", got)
	}
}

// E2E-022: repeating the same tool call triggers stall detection and ends the run.
func TestSmokeAutonomousStallDetection(t *testing.T) {
	t.Parallel()

	sameRead := []byte(`{"path":"same.go"}`)
	ag, store := buildAutonomousAgent(t, autonomousOpts{
		maxSteps: 3,
		budget:   &agent.BudgetConfig{MaxContinuations: 5},
		hookCfg:  hook.TurnContinueConfig{MaxContinuations: 5, StallLimit: 3},
		steps: []llm.ScriptedStep{
			{ToolCalls: []agentkit.ToolCall{{ID: "call-read-1", Name: "read", Input: sameRead}}},
			{ToolCalls: []agentkit.ToolCall{{ID: "call-read-2", Name: "read", Input: sameRead}}},
			{ToolCalls: []agentkit.ToolCall{{ID: "call-read-3", Name: "read", Input: sameRead}}},
			{Text: "should not reach another segment"},
		},
	})

	sessionID := agentkit.SessionID("smoke:stall")
	ctx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, ctx, ag, "stalled 检测冒烟")

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
	if got := agenttest.CountEvents(events, agentkit.EventRunFinish); got != 0 {
		t.Fatalf("run/finish = %d, want 0 when stalled before finish", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventToolCall); got < 3 {
		t.Fatalf("tool/call = %d, want at least 3 repeated reads", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnContinue); got != 0 {
		t.Fatalf("turn/continue = %d, want 0 when stall stops before another segment", got)
	}
}
