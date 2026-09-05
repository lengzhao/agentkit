package loop_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit/build"
)

const testSessionID = agentkit.SessionID("test:default")

func testLoopGraph(t *testing.T, followUpMode agentkit.FollowUpMode, storeDir string) map[string]any {
	t.Helper()
	return map[string]any{
		"loop": map[string]any{
			"use": "loop/default",
			"config": map[string]any{
				"followUpMode": string(followUpMode),
			},
			"deps": map[string]any{
				"agents": []any{
					map[string]any{
						"use": "agent/coding",
						"config": map[string]any{
							"id":       "test",
							"maxSteps": 1,
						},
						"deps": map[string]any{
							"sessionStore": map[string]any{
								"use":    "session/store",
								"config": map[string]any{"dir": "."},
								"deps": map[string]any{
									"workspace": map[string]any{
										"use":    "workspace/default",
										"config": map[string]any{"root": storeDir},
									},
								},
							},
							"llm": map[string]any{
								"use": "llm/scripted",
								"config": map[string]any{
									"steps": []any{
										map[string]any{"text": "ok"},
										map[string]any{"text": "ok"},
										map[string]any{"text": "ok"},
										map[string]any{"text": "ok"},
									},
								},
							},
							"prompt": map[string]any{"use": "prompt/assembler/default"},
							"workspace": map[string]any{
								"use":    "workspace/default",
								"config": map[string]any{"root": storeDir},
							},
							"tools": map[string]any{
								"use": "tools/runtime",
								"deps": map[string]any{
									"tools": []any{},
								},
							},
						},
					},
				},
			},
		},
	}
}

func userMessage(text string) agentkit.ModelMessage {
	return agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}
}

func readUserTexts(t *testing.T, storeDir string, sessionID agentkit.SessionID) []string {
	t.Helper()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(storeDir)})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	sess, err := store.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	msgs, err := sess.DeriveMessages(context.Background())
	if err != nil {
		t.Fatalf("derive messages: %v", err)
	}
	var out []string
	for _, msg := range msgs {
		if msg.Role != "user" || len(msg.Content) == 0 {
			continue
		}
		out = append(out, msg.Content[0].Text)
	}
	return out
}

func testContext(sessionID agentkit.SessionID) context.Context {
	return session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(sessionID), Workspace: string(sessionID)})
}

func loopReq(sessionID agentkit.SessionID, msg agentkit.ModelMessage) agentkit.LoopRequest {
	return agentkit.LoopRequest{
		Event: agentkit.MessageEvent{
			Message: msg,
			Envelope: agentkit.TurnEnvelope{Conversation: string(sessionID)},
		},
	}
}

func TestDispatchDrainsAllFollowUps(t *testing.T) {
	t.Parallel()

	storeDir := t.TempDir()
	loop, _, err := build.Build[agentkit.Loop](context.Background(), testLoopGraph(t, agentkit.FollowUpAll, storeDir), "loop")
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx := testContext(testSessionID)
	if err := loop.FollowUp(ctx, userMessage("follow one")); err != nil {
		t.Fatal(err)
	}
	if err := loop.FollowUp(ctx, userMessage("follow two")); err != nil {
		t.Fatal(err)
	}

	if err := loop.Dispatch(context.Background(), loopReq(testSessionID, userMessage("initial"))); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	got := readUserTexts(t, storeDir, testSessionID)
	want := []string{"initial", "follow one", "follow two"}
	if len(got) < len(want) {
		t.Fatalf("messages = %v, want at least %v", got, want)
	}
	for i, text := range want {
		if got[i] != text {
			t.Fatalf("messages = %v, want prefix %v", got, want)
		}
	}
}

func TestDispatchDrainsOneFollowUpAtATime(t *testing.T) {
	t.Parallel()

	storeDir := t.TempDir()
	loop, _, err := build.Build[agentkit.Loop](context.Background(), testLoopGraph(t, agentkit.FollowUpOneAtATime, storeDir), "loop")
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx := testContext(testSessionID)
	if err := loop.FollowUp(ctx, userMessage("follow one")); err != nil {
		t.Fatal(err)
	}
	if err := loop.FollowUp(ctx, userMessage("follow two")); err != nil {
		t.Fatal(err)
	}

	if err := loop.Dispatch(context.Background(), loopReq(testSessionID, userMessage("initial"))); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	got := readUserTexts(t, storeDir, testSessionID)
	want := []string{"initial", "follow one"}
	if len(got) < len(want) {
		t.Fatalf("messages = %v, want at least %v", got, want)
	}
	for i, text := range want {
		if got[i] != text {
			t.Fatalf("messages = %v, want prefix %v", got, want)
		}
	}

	if err := loop.Dispatch(context.Background(), loopReq(testSessionID, userMessage("second round"))); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	got = readUserTexts(t, storeDir, testSessionID)
	want = []string{"initial", "follow one", "second round", "follow two"}
	if len(got) < len(want) {
		t.Fatalf("messages after second dispatch = %v, want at least %v", got, want)
	}
	for i, text := range want {
		if got[i] != text {
			t.Fatalf("messages after second dispatch = %v, want prefix %v", got, want)
		}
	}
}

func TestDispatchFollowUpTurnLifecycle(t *testing.T) {
	t.Parallel()

	storeDir := t.TempDir()
	loop, _, err := build.Build[agentkit.Loop](context.Background(), testLoopGraph(t, agentkit.FollowUpAll, storeDir), "loop")
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx := testContext(testSessionID)
	if err := loop.FollowUp(ctx, userMessage("follow one")); err != nil {
		t.Fatal(err)
	}
	if err := loop.Dispatch(context.Background(), loopReq(testSessionID, userMessage("initial"))); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(storeDir)})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(context.Background(), testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	turnEnds := 0
	for _, ev := range events {
		if ev.Type == agentkit.EventTurnEnd {
			turnEnds++
		}
	}
	if turnEnds != 2 {
		t.Fatalf("turn/end count = %d, want 2", turnEnds)
	}
}

func TestLoopSteerRoutesToAgent(t *testing.T) {
	t.Parallel()

	storeDir := t.TempDir()
	loop, _, err := build.Build[agentkit.Loop](context.Background(), testLoopGraph(t, agentkit.FollowUpOneAtATime, storeDir), "loop")
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx := testContext(testSessionID)
	if err := loop.Steer(ctx, userMessage("steered")); err != nil {
		t.Fatal(err)
	}

	if err := loop.Dispatch(context.Background(), loopReq(testSessionID, userMessage("initial"))); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	got := readUserTexts(t, storeDir, testSessionID)
	if len(got) < 2 || got[0] != "initial" || got[1] != "steered" {
		t.Fatalf("messages = %v", got)
	}
}

func TestSessionControlsAreIsolated(t *testing.T) {
	t.Parallel()

	sessionA := agentkit.SessionID("session-a")
	sessionB := agentkit.SessionID("session-b")
	storeDirA := t.TempDir()
	storeDirB := t.TempDir()

	loopA, _, err := build.Build[agentkit.Loop](context.Background(), testLoopGraph(t, agentkit.FollowUpAll, storeDirA), "loop")
	if err != nil {
		t.Fatal(err)
	}
	loopB, _, err := build.Build[agentkit.Loop](context.Background(), testLoopGraph(t, agentkit.FollowUpAll, storeDirB), "loop")
	if err != nil {
		t.Fatal(err)
	}

	if err := loopA.FollowUp(testContext(sessionA), userMessage("follow-a")); err != nil {
		t.Fatal(err)
	}
	if err := loopB.FollowUp(testContext(sessionB), userMessage("follow-b")); err != nil {
		t.Fatal(err)
	}

	if err := loopA.Dispatch(context.Background(), loopReq(sessionA, userMessage("turn-a"))); err != nil {
		t.Fatalf("dispatch A: %v", err)
	}
	if err := loopB.Dispatch(context.Background(), loopReq(sessionB, userMessage("turn-b"))); err != nil {
		t.Fatalf("dispatch B: %v", err)
	}

	gotA := readUserTexts(t, storeDirA, sessionA)
	wantA := []string{"turn-a", "follow-a"}
	if len(gotA) < len(wantA) {
		t.Fatalf("session A = %v, want %v", gotA, wantA)
	}
	for i, text := range wantA {
		if gotA[i] != text {
			t.Fatalf("session A = %v, want prefix %v", gotA, wantA)
		}
	}

	gotB := readUserTexts(t, storeDirB, sessionB)
	wantB := []string{"turn-b", "follow-b"}
	if len(gotB) < len(wantB) {
		t.Fatalf("session B = %v, want %v", gotB, wantB)
	}
	for i, text := range wantB {
		if gotB[i] != text {
			t.Fatalf("session B = %v, want prefix %v", gotB, wantB)
		}
	}
}
