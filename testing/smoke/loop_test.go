package smoke_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestSmokeLoopSameSessionSequentialTurns(t *testing.T) {
	t.Parallel()

	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		AgentID: "loop-smoke",
		Steps: []llm.ScriptedStep{
			{Text: "第一段回复。"},
			{Text: "第二段回复。"},
		},
	})
	loopInst, err := loop.New(loop.Config{DefaultAgent: "loop-smoke"}, loop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}

	sessionID := agentkit.SessionID("slack:C-smoke-seq")
	ctx := context.Background()
	msg := func(text string) agentkit.ModelMessage {
		return agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		}
	}
	if err := loopInst.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{SessionID: sessionID, AgentID: "loop-smoke", Message: msg("第一条")},
	}); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if err := loopInst.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{SessionID: sessionID, AgentID: "loop-smoke", Message: msg("第二条")},
	}); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got != 2 {
		t.Fatalf("turn/end = %d, want 2 serialized turns", got)
	}

	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 {
		t.Fatalf("derived messages = %d, want 2 user + 2 assistant", len(messages))
	}
	if agenttest.ContentText(messages[0]) != "第一条" || agenttest.ContentText(messages[2]) != "第二条" {
		t.Fatalf("derived order = [%q, %q]", agenttest.ContentText(messages[0]), agenttest.ContentText(messages[2]))
	}
}

func TestSmokeLoopDifferentSessionsIsolated(t *testing.T) {
	t.Parallel()

	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		AgentID: "loop-smoke",
		Steps: []llm.ScriptedStep{
			{Text: "channel A"},
			{Text: "channel B"},
		},
	})
	loopInst, err := loop.New(loop.Config{DefaultAgent: "loop-smoke"}, loop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	dispatch := func(sessionID agentkit.SessionID, text string) {
		t.Helper()
		if err := loopInst.Dispatch(ctx, agentkit.LoopRequest{
			Event: agentkit.MessageEvent{
				SessionID: sessionID,
				AgentID:   "loop-smoke",
				Message: agentkit.ModelMessage{
					Role:    "user",
					Content: []agentkit.ContentPart{{Type: "text", Text: text}},
				},
			},
		}); err != nil {
			t.Fatalf("dispatch %s: %v", sessionID, err)
		}
	}

	dispatch("slack:C-A", "A 的问题")
	dispatch("slack:C-B", "B 的问题")

	for _, tc := range []struct {
		id   agentkit.SessionID
		want string
	}{
		{"slack:C-A", "A 的问题"},
		{"slack:C-B", "B 的问题"},
	} {
		sess, err := store.Get(ctx, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		messages, err := sess.DeriveMessages(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 2 || agenttest.ContentText(messages[0]) != tc.want {
			t.Fatalf("session %s messages = %+v", tc.id, messages)
		}
	}
}
