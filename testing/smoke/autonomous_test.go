package smoke_test_test

import (
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/plugins/hook"
	"github.com/lengzhao/agentkit/plugins/tool/finish"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	todotool "github.com/lengzhao/agentkit/plugins/tool/todo"
	"github.com/lengzhao/agentkit/runtime/agent"
	rthooks "github.com/lengzhao/agentkit/runtime/hooks"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type autonomousOpts struct {
	hookCfg hook.TurnContinueConfig
	steps   []llm.ScriptedStep
}

func buildAutonomousAgent(t *testing.T, opts autonomousOpts) (agentkit.Agent, agentkit.SessionStore) {
	t.Helper()

	store, _ := agenttest.TempFileStore(t)
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: opts.steps})
	if err != nil {
		t.Fatal(err)
	}
	readPack, err := fs.NewFSMemory(fs.FSMemoryConfig{
		Files: map[string]string{"README.md": "hello"},
		Tools: []string{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := sessevents.New()
	if err != nil {
		t.Fatal(err)
	}
	todoTool, err := todotool.NewTodo(todotool.TodoConfig{}, todotool.TodoDeps{SessionStore: store, SessionEvents: events})
	if err != nil {
		t.Fatal(err)
	}
	finishTool, err := finish.NewFinish(finish.FinishConfig{}, finish.FinishDeps{SessionStore: store, SessionEvents: events})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools:     []agentkit.Tool{todoTool, finishTool},
		ToolPacks: []agentkit.ToolPack{readPack},
	})
	if err != nil {
		t.Fatal(err)
	}
	tc, err := hook.NewTurnContinue(opts.hookCfg, hook.TurnContinueDeps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	hooksRT, err := rthooks.New(rthooks.Config{}, rthooks.Deps{
		Providers: []agentkit.HookProvider{tc},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}

	ag, err := agent.New(agent.Config{ID: "smoke"}, agent.Deps{
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
		var data capsession.TurnContinueData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			t.Fatalf("decode turn/continue: %v", err)
		}
		reasons = append(reasons, data.Reason)
	}
	return reasons
}

// E2E-020: turn-continue extends the turn when todos remain unfinished.
func TestSmokeAutonomousContinuation(t *testing.T) {
	t.Parallel()

	ag, store := buildAutonomousAgent(t, autonomousOpts{
		hookCfg: hook.TurnContinueConfig{MaxContinuations: 3, StallLimit: 10},
		steps: []llm.ScriptedStep{
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-todo", Name: "todo",
				Input: []byte(`{"op":"set","items":[{"id":"1","title":"read README","status":"pending"}]}`),
			}}},
			{Text: "先列了任务，下一步去读文件。"},
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-read", Name: "read", Input: []byte(`{"path":"README.md"}`),
			}}},
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-done", Name: "todo", Input: []byte(`{"op":"complete","ids":["1"]}`),
			}}},
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-finish", Name: "finish",
				Input: []byte(`{"status":"completed","summary":"smoke done"}`),
			}}},
			{Text: "完成。"},
		},
	})

	sessionID := agentkit.SessionID("smoke:continue")
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
	for _, reason := range reasons {
		if reason != string(agentkit.StopNoToolCalls) {
			t.Fatalf("turn/continue reasons = %v, want no-tool-calls", reasons)
		}
	}
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
}

// E2E-022: repeating the same tool call triggers stall detection and ends the run.
func TestSmokeAutonomousStallDetection(t *testing.T) {
	t.Parallel()

	sameRead := []byte(`{"path":"same.go"}`)
	ag, store := buildAutonomousAgent(t, autonomousOpts{
		hookCfg: hook.TurnContinueConfig{MaxContinuations: 5, StallLimit: 3},
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
	if got := agenttest.CountEvents(events, agentkit.EventRunFinish); got != 0 {
		t.Fatalf("run/finish = %d, want 0 on stall", got)
	}
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
}
