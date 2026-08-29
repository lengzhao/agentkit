package smoke_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestSmokeRecoverySynthesizesOrphanToolResult(t *testing.T) {
	t.Parallel()

	store, _ := agenttest.TempFileStore(t)
	sessionID := agentkit.SessionID("smoke:orphan-read")
	agenttest.SeedCrashedToolCall(t, store, sessionID, "smoke", agentkit.ToolCall{
		ID: "call-read", Name: "read", Input: []byte(`{"path":"README.md"}`),
	}, "read the file")

	readPack, err := fs.NewFSMemory(fs.FSMemoryConfig{
		Files: map[string]string{"README.md": "recovered content"},
		Tools: []string{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	toolRT := agenttest.ToolsRuntime(t, tools.RuntimeDeps{ToolPacks: []agentkit.ToolPack{readPack}})
	ag, _ := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Store: store,
		Steps: []llm.ScriptedStep{{Text: "恢复后继续。"}},
		Tools: toolRT,
	})

	ctx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, ctx, ag, "continue")

	events := agenttest.SessionEvents(t, context.Background(), store, sessionID)
	if got := agenttest.CountEvents(events, agentkit.EventSessionRecovery); got != 1 {
		t.Fatalf("session/recovery = %d, want 1", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got != 2 {
		t.Fatalf("turn/end = %d, want repaired + new turn", got)
	}
	agenttest.AssertToolResultContains(t, events, "call-read", "interrupted")

	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	agenttest.AssertDeriveMessagesToolCallsAnswered(t, sess, ctx)
}

func TestSmokeCleanSessionSkipsRecovery(t *testing.T) {
	t.Parallel()

	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{{Text: "正常结束。"}},
	})
	sessionID := agentkit.SessionID("smoke:clean")
	ctx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, ctx, ag, "hello")

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	if got := agenttest.CountEvents(events, agentkit.EventSessionRecovery); got != 0 {
		t.Fatalf("session/recovery = %d, want 0 on clean turn", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got != 1 {
		t.Fatalf("turn/end = %d, want 1", got)
	}
}
