package smoke_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// Smoke tests run keyless scripted flows end-to-end: parent agent → delegate tool
// → in-process subagent → session replay. They guard regressions such as subagent
// recovery closing a parent turn mid-delegate or duplicate delegate tool results.

func TestSmokeSubagentDelegateEndToEnd(t *testing.T) {
	t.Parallel()

	env := agenttest.NewSubagentDelegateEnv(t, agenttest.SubagentDelegateConfig{})
	ctx := agenttest.TurnContext(env.LogicalID, agentkit.AgentID("nex"))
	agenttest.RunTurn(t, ctx, env.Agent, "调研一下 loop 串行机制")

	events := agenttest.SessionEvents(t, ctx, env.Store, env.LogicalID)
	agenttest.AssertSubagentParentSession(t, events)

	childID := agenttest.FindChildSessionID(t, events)
	childEvents := agenttest.SessionEvents(t, ctx, env.Store, childID)
	if got := agenttest.CountEvents(childEvents, agentkit.EventTurnStart); got != 1 {
		t.Fatalf("child turn/start = %d, want 1", got)
	}
	if got := agenttest.CountEvents(childEvents, agentkit.EventSessionRecovery); got != 0 {
		t.Fatalf("child session/recovery = %d, want 0", got)
	}
}

func TestSmokeSubagentDelegateWithLogicalStoreSession(t *testing.T) {
	t.Parallel()

	logical := agentkit.SessionID("chat-api:nex-channel")
	env := agenttest.NewSubagentDelegateEnv(t, agenttest.SubagentDelegateConfig{LogicalID: logical})

	ctx := agenttest.LoopTurnContext(logical, agentkit.AgentID("nex"))
	agenttest.RunTurn(t, ctx, env.Agent, "调研一下 loop 串行机制")

	events := agenttest.SessionEvents(t, ctx, env.Store, logical)
	agenttest.AssertSubagentParentSession(t, events)
	agenttest.AssertNoToolResultWithContent(t, events, "call-delegate", "interrupted")

	sess, err := env.Store.Get(ctx, logical)
	if err != nil {
		t.Fatal(err)
	}
	agenttest.AssertDeriveMessagesToolCallsAnswered(t, sess, ctx)
}

func TestSmokeSubagentDelegateViaLoopWithStoreSession(t *testing.T) {
	t.Parallel()

	logical := agentkit.SessionID("chat-api:nex-channel")
	env := agenttest.NewSubagentDelegateEnv(t, agenttest.SubagentDelegateConfig{LogicalID: logical})

	loopInst, err := loop.New(loop.Config{DefaultAgent: "nex"}, loop.Deps{Agents: []agentkit.Agent{env.Agent}})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := loopInst.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{
			SessionID: logical,
			AgentID:   "nex",
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "委派 researcher"}},
			},
		},
	}); err != nil {
		t.Fatalf("loop dispatch: %v", err)
	}

	ctx = agenttest.LoopTurnContext(logical, agentkit.AgentID("nex"))
	events := agenttest.SessionEvents(t, ctx, env.Store, logical)
	agenttest.AssertSubagentParentSession(t, events)
	agenttest.AssertNoToolResultWithContent(t, events, "call-delegate", "interrupted")
}

func TestSmokeSubagentDelegateWithoutStoreSessionMapping(t *testing.T) {
	t.Parallel()

	logical := agentkit.SessionID("cli:smoke-delegate")
	env := agenttest.NewSubagentDelegateEnv(t, agenttest.SubagentDelegateConfig{LogicalID: logical})
	ctx := agenttest.TurnContext(logical, agentkit.AgentID("nex"))
	agenttest.RunTurn(t, ctx, env.Agent, "调研一下 loop 串行机制")

	events := agenttest.SessionEvents(t, ctx, env.Store, logical)
	agenttest.AssertSubagentParentSession(t, events)

	sess, err := env.Store.Get(ctx, logical)
	if err != nil {
		t.Fatal(err)
	}
	agenttest.AssertDeriveMessagesToolCallsAnswered(t, sess, ctx)
}

func TestSmokeSessionRecoveryAfterCrash(t *testing.T) {
	t.Parallel()

	store, _ := agenttest.TempFileStore(t)
	sessionID := agentkit.SessionID("smoke:crash")
	ctx := context.Background()
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendTurnStart(ctx, sess, "nex"); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, "nex", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "read file"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendStepStart(ctx, sess, "nex", 0); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, "nex", agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:      "assistant",
		ToolCalls: []agentkit.ToolCall{{ID: "call-read", Name: "read", Input: []byte(`{"path":"README.md"}`)}},
	}); err != nil {
		t.Fatal(err)
	}

	provider := agenttest.MustScripted(t, llm.ScriptedStep{Text: "已恢复并继续。"})
	ag, err := agent.New(agent.Config{ID: "nex", MaxSteps: 3}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        agenttest.EmptyToolsRuntime(t),
		Prompt:       agenttest.DefaultAssembler(t),
		Workspace:    agenttest.TestWorkspace(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	runCtx := agenttest.TurnContext(sessionID, agentkit.AgentID("nex"))
	agenttest.RunTurn(t, runCtx, ag, "continue")

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	if got := agenttest.CountEvents(events, agentkit.EventSessionRecovery); got != 1 {
		t.Fatalf("session/recovery = %d, want 1", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got != 2 {
		t.Fatalf("turn/end = %d, want repaired + new turn", got)
	}
	agenttest.AssertDeriveMessagesToolCallsAnswered(t, sess, ctx)
}
