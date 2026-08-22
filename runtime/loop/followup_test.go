package loop_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit/build"
)

func testLoopGraph(t *testing.T, followUpMode agentkit.FollowUpMode, sessionPath string) map[string]any {
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
							"session": map[string]any{
								"use":    "session/jsonl",
								"config": map[string]any{"path": sessionPath},
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

func readUserTexts(t *testing.T, sessionPath string) []string {
	t.Helper()
	sess, _, err := build.Build[agentkit.Session](context.Background(), map[string]any{
		"session": map[string]any{
			"use":    "session/jsonl",
			"config": map[string]any{"path": sessionPath},
		},
	}, "session")
	if err != nil {
		t.Fatalf("build session: %v", err)
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

func TestDispatchDrainsAllFollowUps(t *testing.T) {
	t.Parallel()

	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	loop, _, err := build.Build[agentkit.Loop](context.Background(), testLoopGraph(t, agentkit.FollowUpAll, sessionPath), "loop")
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx := context.Background()
	if err := loop.FollowUp(ctx, agentkit.SessionControlRequest{
		Message: userMessage("follow one"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := loop.FollowUp(ctx, agentkit.SessionControlRequest{
		Message: userMessage("follow two"),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := loop.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{Message: userMessage("initial")},
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	got := readUserTexts(t, sessionPath)
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

	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	loop, _, err := build.Build[agentkit.Loop](context.Background(), testLoopGraph(t, agentkit.FollowUpOneAtATime, sessionPath), "loop")
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx := context.Background()
	if err := loop.FollowUp(ctx, agentkit.SessionControlRequest{
		Message: userMessage("follow one"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := loop.FollowUp(ctx, agentkit.SessionControlRequest{
		Message: userMessage("follow two"),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := loop.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{Message: userMessage("initial")},
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	got := readUserTexts(t, sessionPath)
	want := []string{"initial", "follow one"}
	if len(got) < len(want) {
		t.Fatalf("messages = %v, want at least %v", got, want)
	}
	for i, text := range want {
		if got[i] != text {
			t.Fatalf("messages = %v, want prefix %v", got, want)
		}
	}

	if _, err := loop.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{Message: userMessage("second round")},
	}); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	got = readUserTexts(t, sessionPath)
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

	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	loop, _, err := build.Build[agentkit.Loop](context.Background(), testLoopGraph(t, agentkit.FollowUpAll, sessionPath), "loop")
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx := context.Background()
	if err := loop.FollowUp(ctx, agentkit.SessionControlRequest{
		Message: userMessage("follow one"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{Message: userMessage("initial")},
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	sess, _, err := build.Build[agentkit.Session](context.Background(), map[string]any{
		"session": map[string]any{
			"use":    "session/jsonl",
			"config": map[string]any{"path": sessionPath},
		},
	}, "session")
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

	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	loop, _, err := build.Build[agentkit.Loop](context.Background(), testLoopGraph(t, agentkit.FollowUpOneAtATime, sessionPath), "loop")
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx := context.Background()
	if err := loop.Steer(ctx, agentkit.SessionControlRequest{
		Message: userMessage("steered"),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := loop.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{Message: userMessage("initial")},
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	got := readUserTexts(t, sessionPath)
	if len(got) < 2 || got[0] != "initial" || got[1] != "steered" {
		t.Fatalf("messages = %v", got)
	}
}

func TestSessionControlsAreIsolated(t *testing.T) {
	t.Parallel()

	sessionA := filepath.Join(t.TempDir(), "a.jsonl")
	sessionB := filepath.Join(t.TempDir(), "b.jsonl")

	graphA := testLoopGraph(t, agentkit.FollowUpAll, sessionA)
	graphB := testLoopGraph(t, agentkit.FollowUpAll, sessionB)

	loopA, _, err := build.Build[agentkit.Loop](context.Background(), graphA, "loop")
	if err != nil {
		t.Fatal(err)
	}
	loopB, _, err := build.Build[agentkit.Loop](context.Background(), graphB, "loop")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := loopA.FollowUp(ctx, agentkit.SessionControlRequest{
		SessionID: "session-a",
		Message:   userMessage("follow-a"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := loopB.FollowUp(ctx, agentkit.SessionControlRequest{
		SessionID: "session-b",
		Message:   userMessage("follow-b"),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := loopA.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{
			SessionID: "session-a",
			Message:   userMessage("turn-a"),
		},
	}); err != nil {
		t.Fatalf("dispatch A: %v", err)
	}
	if _, err := loopB.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{
			SessionID: "session-b",
			Message:   userMessage("turn-b"),
		},
	}); err != nil {
		t.Fatalf("dispatch B: %v", err)
	}

	gotA := readUserTexts(t, sessionA)
	wantA := []string{"turn-a", "follow-a"}
	if len(gotA) < len(wantA) {
		t.Fatalf("session A = %v, want %v", gotA, wantA)
	}
	for i, text := range wantA {
		if gotA[i] != text {
			t.Fatalf("session A = %v, want prefix %v", gotA, wantA)
		}
	}

	gotB := readUserTexts(t, sessionB)
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
