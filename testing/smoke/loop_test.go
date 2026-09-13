package smoke_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/session"
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
	if err := loopInst.Dispatch(ctx, agenttest.LoopRequest(sessionID, agentkit.MessageEvent{AgentID: "loop-smoke", Message: msg("第一条")})); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if err := loopInst.Dispatch(ctx, agenttest.LoopRequest(sessionID, agentkit.MessageEvent{AgentID: "loop-smoke", Message: msg("第二条")})); err != nil {
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

// TestSmokeLoopSteerWhileBusy simulates Lark/chat busy-session steering: a second
// user message arrives while Dispatch still holds the session lock; Loop must
// drain steering after the first turn and run a second turn in the same Dispatch.
func TestSmokeLoopSteerWhileBusy(t *testing.T) {
	t.Parallel()

	hold := make(chan struct{}, 2)
	ag := &gateAgent{hold: hold}
	loopInst, err := loop.New(loop.Config{DefaultAgent: ag.ID()}, loop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}

	sessionID := agentkit.SessionID("lark:smoke:busy-steer")
	steerCtx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(sessionID),
		Workspace:    string(sessionID),
	})
	msg := func(text string) agentkit.ModelMessage {
		return agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- loopInst.Dispatch(context.Background(), agenttest.LoopRequest(sessionID, agentkit.MessageEvent{
			AgentID: ag.ID(),
			Message: msg("第一条"),
		}))
	}()

	waitUntilSmoke(t, func() bool { return loopInst.IsSessionBusy(sessionID) })

	if err := loopInst.Steer(steerCtx, msg("第二条")); err != nil {
		t.Fatalf("steer: %v", err)
	}
	hold <- struct{}{} // finish first turn
	waitUntilSmoke(t, func() bool { return ag.turns() >= 2 })
	hold <- struct{}{} // finish steered turn

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch did not finish after steer")
	}
	if loopInst.IsSessionBusy(sessionID) {
		t.Fatal("session still busy after dispatch")
	}
	if got := ag.turns(); got != 2 {
		t.Fatalf("RunTurn calls = %d, want 2 (initial + steered)", got)
	}
}

type gateAgent struct {
	hold chan struct{}

	mu    sync.Mutex
	calls int
}

func (a *gateAgent) ID() agentkit.AgentID { return "gate-smoke" }

func (a *gateAgent) turns() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func (a *gateAgent) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	select {
	case <-a.hold:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitUntilSmoke(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
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
		if err := loopInst.Dispatch(ctx, agenttest.LoopRequest(sessionID, agentkit.MessageEvent{
			AgentID: "loop-smoke",
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: text}},
			},
		})); err != nil {
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
