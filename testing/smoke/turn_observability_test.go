package smoke_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// E2E-400: a single tool turn emits the minimal session audit skeleton.
func TestSmokeTurnSessionEventSkeleton(t *testing.T) {
	t.Parallel()

	readPack, err := fs.NewFSMemory(fs.FSMemoryConfig{
		Files: map[string]string{"README.md": "hello"},
		Tools: []string{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	toolRT := agenttest.ToolsRuntime(t, tools.RuntimeDeps{
		ToolPacks: []agentkit.ToolPack{readPack},
		Approval:  agenttest.AllowAll{},
	})
	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-read", Name: "read", Input: []byte(`{"path":"README.md"}`),
			}}},
			{Text: "done"},
		},
		Tools: toolRT,
	})

	sessionID := agentkit.SessionID("smoke:turn-skeleton")
	ctx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, ctx, ag, "读 README")

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnStart, 1)
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
	agenttest.AssertEventAtLeast(t, events, agentkit.EventStepStart, 1)
	agenttest.AssertEventAtLeast(t, events, agentkit.EventToolResult, 1)
	agenttest.AssertEventAtLeast(t, events, agentkit.EventStepEnd, 1)

	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	agenttest.AssertDeriveMessagesToolCallsAnswered(t, sess, ctx)
}
