package loop_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/pluginkit/build"
)

func TestDispatchRoutesBySessionID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}

	graph := map[string]any{
		"loop": map[string]any{
			"use": "loop/default",
			"deps": map[string]any{
				"agents": []any{
					map[string]any{
						"use": "agent/coding",
						"config": map[string]any{
							"id":       "test",
							"maxSteps": 2,
						},
						"deps": map[string]any{
							"sessionStore": map[string]any{
								"use":    "session/store",
								"config": map[string]any{"dir": "."},
								"deps": map[string]any{
									"workspace": map[string]any{
										"use":    "workspace/default",
										"config": map[string]any{"root": dir},
									},
								},
							},
							"llm": map[string]any{
								"use": "llm/scripted",
								"config": map[string]any{
									"steps": []any{
										map[string]any{"text": "ok"},
										map[string]any{"text": "ok"},
									},
								},
							},
							"prompt": map[string]any{"use": "prompt/assembler/default"},
							"workspace": map[string]any{
								"use":    "workspace/default",
								"config": map[string]any{"root": dir},
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

	loop, _, err := build.Build[agentkit.Loop](context.Background(), graph, "loop")
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx := context.Background()
	msg := func(text string) agentkit.ModelMessage {
		return agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		}
	}

	if err := loop.Dispatch(ctx, agenttest.LoopRequest("slack:C001", agentkit.MessageEvent{
		Message: msg("channel one"),
	})); err != nil {
		t.Fatalf("dispatch C001: %v", err)
	}
	if err := loop.Dispatch(ctx, agenttest.LoopRequest("slack:C002", agentkit.MessageEvent{
		Message: msg("channel two"),
	})); err != nil {
		t.Fatalf("dispatch C002: %v", err)
	}

	s1, err := store.Get(ctx, "slack:C001")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := store.Get(ctx, "slack:C002")
	if err != nil {
		t.Fatal(err)
	}
	msgs1, err := s1.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	msgs2, err := s2.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs1) != 2 || msgs1[0].Content[0].Text != "channel one" {
		t.Fatalf("C001 messages: %+v", msgs1)
	}
	if len(msgs2) != 2 || msgs2[0].Content[0].Text != "channel two" {
		t.Fatalf("C002 messages: %+v", msgs2)
	}
}
