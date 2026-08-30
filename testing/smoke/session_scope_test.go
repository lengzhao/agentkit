package smoke_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// E2E-512: runner sessionScope folds platform delivery IDs before Loop locking and storage.
func TestSmokeSessionScopeChannelFoldsDeliveries(t *testing.T) {
	t.Parallel()

	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{{Text: "ok"}, {Text: "ok"}},
	})
	loopInst, err := loop.New(loop.Config{DefaultAgent: "smoke"}, loop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}

	delivery1 := session.BuildDeliverySessionID("slack", "C001", "111.0", "U111")
	delivery2 := session.BuildDeliverySessionID("slack", "C001", "222.0", "U222")
	effective := session.ApplyScope(delivery1, session.ScopeChannel, "U111")
	if other := session.ApplyScope(delivery2, session.ScopeChannel, "U222"); other != effective {
		t.Fatalf("effective ids differ: %q vs %q", effective, other)
	}

	ctx := context.Background()
	for i, ev := range []agentkit.MessageEvent{
		{
			SessionID: delivery1,
			UserID:    "U111",
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "one"}},
			},
		},
		{
			SessionID: delivery2,
			UserID:    "U222",
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "two"}},
			},
		},
	} {
		_ = i
		if err := loopInst.Dispatch(ctx, agentkit.LoopRequest{
			StoreSessionID: effective,
			Event: agentkit.MessageEvent{
				SessionID: ev.SessionID,
				UserID:    ev.UserID,
				AgentID:   "smoke",
				Message:   ev.Message,
			},
		}); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
	}

	events := agenttest.SessionEvents(t, ctx, store, effective)
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got != 2 {
		t.Fatalf("turn/end = %d, want 2 turns in one channel session", got)
	}
	if got := agenttest.CountEvents(events, agentkit.EventUserMessage); got != 2 {
		t.Fatalf("user messages = %d, want 2", got)
	}
}

func TestSmokeSessionScopeThreadSplitsDeliveries(t *testing.T) {
	t.Parallel()

	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{{Text: "ok"}, {Text: "ok"}},
	})
	loopInst, err := loop.New(loop.Config{DefaultAgent: "smoke"}, loop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}

	delivery1 := session.BuildDeliverySessionID("slack", "C001", "111.0", "U111")
	delivery2 := session.BuildDeliverySessionID("slack", "C001", "222.0", "U222")
	effective1 := session.ApplyScope(delivery1, session.ScopeThread, "U111")
	effective2 := session.ApplyScope(delivery2, session.ScopeThread, "U222")
	if effective1 == effective2 {
		t.Fatalf("thread scope collapsed distinct threads: %q", effective1)
	}

	ctx := context.Background()
	if err := loopInst.Dispatch(ctx, agentkit.LoopRequest{
		StoreSessionID: effective1,
		Event: agentkit.MessageEvent{
			SessionID: delivery1,
			UserID:    "U111",
			AgentID:   "smoke",
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "thread one"}},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := loopInst.Dispatch(ctx, agentkit.LoopRequest{
		StoreSessionID: effective2,
		Event: agentkit.MessageEvent{
			SessionID: delivery2,
			UserID:    "U222",
			AgentID:   "smoke",
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "thread two"}},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if got := agenttest.CountEvents(agenttest.SessionEvents(t, ctx, store, effective1), agentkit.EventTurnEnd); got != 1 {
		t.Fatalf("thread1 turn/end = %d, want 1", got)
	}
	if got := agenttest.CountEvents(agenttest.SessionEvents(t, ctx, store, effective2), agentkit.EventTurnEnd); got != 1 {
		t.Fatalf("thread2 turn/end = %d, want 1", got)
	}
}
