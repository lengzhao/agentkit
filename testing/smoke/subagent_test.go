package smoke_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestSmokeSubagentSecondParentTurn(t *testing.T) {
	t.Parallel()

	steps := append(agenttest.DefaultSubagentSmokeSteps(),
		llm.ScriptedStep{Text: "第二轮：委派已完成，继续回答用户。"},
	)
	env := agenttest.NewSubagentDelegateEnv(t, agenttest.SubagentDelegateConfig{Steps: steps})
	ctx := agenttest.TurnContext(env.LogicalID, agentkit.AgentID("nex"))

	agenttest.RunTurn(t, ctx, env.Agent, "第一轮：委派 researcher")
	agenttest.RunTurn(t, ctx, env.Agent, "第二轮：还有补充吗")

	events := agenttest.SessionEvents(t, ctx, env.Store, env.LogicalID)
	if got := agenttest.CountEvents(events, agentkit.EventSubagentStart); got != 1 {
		t.Fatalf("subagent/start = %d, want delegate only in first turn", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventSessionRecovery); got != 0 {
		t.Fatalf("parent session/recovery = %d, want 0", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got != 2 {
		t.Fatalf("parent turn/end = %d, want 2 sequential turns", got)
	}
	agenttest.AssertNoToolResultWithContent(t, events, "call-delegate", "interrupted")
	agenttest.AssertToolResultContains(t, events, "call-delegate", "串行执行")
}

func TestSmokeSubagentChildEventsStayInChildSession(t *testing.T) {
	t.Parallel()

	env := agenttest.NewSubagentDelegateEnv(t, agenttest.SubagentDelegateConfig{})
	ctx := agenttest.TurnContext(env.LogicalID, agentkit.AgentID("nex"))
	agenttest.RunTurn(t, ctx, env.Agent, "委派 researcher")

	parentEvents := agenttest.SessionEvents(t, ctx, env.Store, env.LogicalID)
	childID := agenttest.FindChildSessionID(t, parentEvents)
	childEvents := agenttest.SessionEvents(t, ctx, env.Store, childID)

	if got := agenttest.CountEvents(childEvents, agentkit.EventToolResult); got < 1 {
		t.Fatalf("child tool/result = %d, want finish result", got)
	}
	for _, ev := range parentEvents {
		if ev.Type != agentkit.EventToolResult {
			continue
		}
		result := agenttest.ToolResult(t, ev)
		if result.ID == "call-finish" {
			t.Fatalf("child finish result leaked into parent session: %+v", result)
		}
	}
}
