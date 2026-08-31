package agentkit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/settings"
	"github.com/lengzhao/agentkit/cap/workspace"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit/build"
)

func TestCompactionPruneToolResults(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100)
	messages := []agentkit.ModelMessage{{
		Role: "tool",
		ToolResults: []agentkit.ToolResult{{
			ID:      "call-1",
			Name:    "read",
			Content: long,
		}},
	}}
	pruned := compaction.PruneToolResults(messages, 20)
	if len(pruned[0].ToolResults[0].Content) >= len(long) {
		t.Fatalf("expected truncated tool result, got len=%d", len(pruned[0].ToolResults[0].Content))
	}
}

func TestSkillToolLoadsSkill(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Demo\nDo the demo."), 0o644); err != nil {
		t.Fatal(err)
	}

	sessionDir := t.TempDir()
	sessionID := agentkit.SessionID("test:default")
	sessionStoreCfg := map[string]any{
		"use":    "session/store",
		"config": map[string]any{"dir": "."},
		"deps": map[string]any{
			"workspace": map[string]any{
				"use":    "workspace/default",
				"config": map[string]any{"root": sessionDir},
			},
		},
	}
	workspaceCfg := map[string]any{
		"use":    "workspace/default",
		"config": map[string]any{"root": dir},
	}

	graph := map[string]any{
		"agent": map[string]any{
			"use": "agent/coding",
			"config": map[string]any{
				"id":       "test",
				"maxSteps": 5,
			},
			"deps": map[string]any{
				"sessionStore": sessionStoreCfg,
				"llm": map[string]any{
					"use": "llm/scripted",
					"config": map[string]any{
						"steps": []any{
							map[string]any{
								"text": "",
								"toolCalls": []any{
									map[string]any{
										"id":    "call-1",
										"name":  "skill",
										"input": `{"name":"demo-skill"}`,
									},
								},
							},
							map[string]any{"text": "loaded"},
						},
					},
				},
				"prompt": map[string]any{"use": "prompt/assembler/default"},
				"workspace": workspaceCfg,
				"tools": map[string]any{
					"use": "tools/runtime",
					"deps": map[string]any{
						"tools": []any{
							map[string]any{
								"use": "tool/skill",
								"deps": map[string]any{
									"sessionStore": sessionStoreCfg,
									"skills": map[string]any{
										"use": "skill/filesystem",
										"config": map[string]any{
											"dirs": []string{"."},
										},
										"deps": map[string]any{
											"workspace": workspaceCfg,
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
			Content: []agentkit.ContentPart{{Type: "text", Text: "load skill"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(sessionDir)})
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
	found := false
	for _, msg := range replay {
		for _, part := range msg.Content {
			if strings.Contains(part.Text, "<skill name=\"demo-skill\">") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected skill load event in derived messages")
	}
}

func TestCredentialsEnvResolve(t *testing.T) {
	t.Setenv("AGENTKIT_TEST_SECRET", "secret-value")
	graph := map[string]any{
		"creds": map[string]any{"use": "credentials/env"},
	}
	store, _, err := build.Build[credentials.Store](context.Background(), graph, "creds")
	if err != nil {
		t.Fatalf("build credentials: %v", err)
	}
	secret, err := store.Resolve(context.Background(), "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if secret.Value != "secret-value" {
		t.Fatalf("unexpected secret value: %q", secret.Value)
	}
}

func TestSettingsFileGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte("model:\n  name: gpt-test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	graph := map[string]any{
		"settings": map[string]any{
			"use": "settings/file",
			"config": map[string]any{
				"path": path,
			},
		},
	}
	store, _, err := build.Build[settings.Store](context.Background(), graph, "settings")
	if err != nil {
		t.Fatalf("build settings: %v", err)
	}
	value, err := store.Get(context.Background(), "model.name")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if value.Raw != "gpt-test" {
		t.Fatalf("unexpected model name: %v", value.Raw)
	}
}
