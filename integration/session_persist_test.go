//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// E2E-300: jsonl session survives a new store/agent instance (simulated restart).
func TestIntegrationJSONLSessionPersistsAcrossRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("integration session persist")
	}

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	openStore := func() agentkit.SessionStore {
		store, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: ws})
		if err != nil {
			t.Fatalf("session store: %v", err)
		}
		return store
	}

	sessionID := agentkit.SessionID("cli:persist-restart")
	ctx := context.Background()

	store1 := openStore()
	ag1, _ := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Store: store1,
		Steps: []llm.ScriptedStep{
			{Text: "第一轮回复。"},
		},
	})
	turnCtx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, turnCtx, ag1, "记住：第一轮")

	// Simulate process restart: new store handle, same on-disk jsonl.
	store2 := openStore()
	eventsAfterRestart := agenttest.SessionEvents(t, ctx, store2, sessionID)
	if got := agenttest.CountEvents(eventsAfterRestart, agentkit.EventUserMessage); got < 1 {
		t.Fatalf("user/message after restart = %d, want at least 1", got)
	}
	if got := agenttest.CountEvents(eventsAfterRestart, agentkit.EventAssistantMessage); got < 1 {
		t.Fatalf("assistant/message after restart = %d, want at least 1", got)
	}

	ag2, _ := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Store: store2,
		Steps: []llm.ScriptedStep{
			{Text: "第二轮回复。"},
		},
	})
	agenttest.RunTurn(t, turnCtx, ag2, "继续：第二轮")

	events := agenttest.SessionEvents(t, ctx, store2, sessionID)
	if got := agenttest.CountEvents(events, agentkit.EventUserMessage); got < 2 {
		t.Fatalf("user/message = %d, want at least 2 across turns", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got < 2 {
		t.Fatalf("turn/end = %d, want at least 2", got)
	}

	sess, err := store2.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 4 {
		t.Fatalf("derived messages = %d, want history from both turns", len(messages))
	}
	foundFirst := false
	for _, msg := range messages {
		if msg.Role == "user" && agenttest.ContentText(msg) == "记住：第一轮" {
			foundFirst = true
		}
	}
	if !foundFirst {
		t.Fatal("derived history missing first turn user message")
	}
	agenttest.AssertDeriveMessagesToolCallsAnswered(t, sess, ctx)
}
