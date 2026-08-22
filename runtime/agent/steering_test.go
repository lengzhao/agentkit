package agent_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/sessioncontrol"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
)

func TestSteerInjectsBeforeNextStep(t *testing.T) {
	t.Parallel()

	graph := map[string]any{
		"agent": map[string]any{
			"use": "agent/coding",
			"config": map[string]any{
				"id":       "test",
				"maxSteps": 5,
			},
			"deps": map[string]any{
				"session": map[string]any{"use": "session/memory"},
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

	ctx := context.Background()
	ctrl := sessioncontrol.New()
	if err := ctrl.Steer(ctx, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "steered"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, err = ag.RunTurn(ctx, agentkit.TurnInput{
		Control: ctrl,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "start"}},
		},
	})
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	msgs, err := ag.Session().DeriveMessages(ctx)
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
