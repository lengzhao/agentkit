package agentkit_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit/build"
)

func TestCodingAgentReadsFile(t *testing.T) {
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
							map[string]any{
								"text": "",
								"toolCalls": []any{
									map[string]any{
										"id":    "call-1",
										"name":  "read",
										"input": `{"path":"README.md"}`,
									},
								},
							},
							map[string]any{
								"text": "README says hello",
							},
						},
					},
				},
				"prompt": map[string]any{
					"use": "prompt/assembler/default",
				},
				"tools": map[string]any{
					"use": "tools/runtime",
					"deps": map[string]any{
						"tools": []any{
							map[string]any{
								"use": "tool/read-file",
								"deps": map[string]any{
									"fs": map[string]any{
										"use": "fs/memory",
										"config": map[string]any{
											"files": map[string]string{
												"README.md": "hello from readme",
											},
										},
									},
								},
							},
						},
						"approval": map[string]any{"use": "approval/auto-deny"},
					},
				},
			},
		},
	}

	ag, _, err := build.Build[agentkit.Agent](context.Background(), graph, "agent")
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, sessionID)
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "read README"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	replay, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatalf("derive messages: %v", err)
	}
	if len(replay) == 0 {
		raw, _ := json.Marshal(replay)
		t.Fatalf("expected replay messages, got %s", string(raw))
	}
	last := replay[len(replay)-1]
	if last.Role != "assistant" {
		t.Fatalf("expected final assistant message, got %q", last.Role)
	}
	if text := contentText(last); text != "README says hello" {
		t.Fatalf("unexpected assistant text: %q", text)
	}
}

func contentText(msg agentkit.ModelMessage) string {
	var b []byte
	for _, part := range msg.Content {
		if part.Type == "text" {
			b = append(b, part.Text...)
		}
	}
	return string(b)
}
