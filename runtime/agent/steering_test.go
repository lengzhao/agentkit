package agent_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit/build"
)

func TestSteerInjectsBeforeNextStep(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sessionID := agentkit.SessionID("test:default")
	graph := map[string]any{
		"agent": map[string]any{
			"use": "agent/coding",
			"config": map[string]any{
				"id":       "test",
				"maxSteps": 5,
			},
			"deps": map[string]any{
				"sessionStore": map[string]any{
					"use":    "session/store",
					"config": map[string]any{"dir": dir},
				},
				"llm": map[string]any{
					"use": "llm/scripted",
					"config": map[string]any{
						"steps": []any{
							map[string]any{"text": "first"},
							map[string]any{"text": "second"},
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
	}

	ag, _, err := build.Build[agentkit.Agent](context.Background(), graph, "agent")
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}

	ctrl := loop.NewControl()
	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, sessionID)
	ctx = context.WithValue(ctx, agentkit.KeySessionControl, ctrl)
	if err := ctrl.Steer(ctx, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "steered"}},
	}); err != nil {
		t.Fatal(err)
	}

	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "start"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	store, err := session.NewStore(session.StoreConfig{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundStart := false
	foundSteer := false
	for _, msg := range msgs {
		if len(msg.Content) == 0 {
			continue
		}
		switch msg.Content[0].Text {
		case "start":
			foundStart = true
		case "steered":
			foundSteer = true
		}
	}
	if !foundStart || !foundSteer {
		t.Fatalf("expected start and steered messages, got %+v", msgs)
	}
}
