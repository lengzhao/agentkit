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
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
)

func TestCompactionPruneToolResults(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100)
	messages := []agentkit.ModelMessage{{
		Role: "tool",
		Content: []agentkit.ContentPart{{
			Type: "text",
			Text: long,
		}},
	}}
	pruned := compaction.PruneToolResults(messages, 20)
	if len(pruned[0].Content[0].Text) >= len(long) {
		t.Fatalf("expected truncated tool result, got len=%d", len(pruned[0].Content[0].Text))
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
				"tools": map[string]any{
					"use": "tools/runtime",
					"deps": map[string]any{
						"tools": []any{
							map[string]any{
								"use": "tool/skill",
								"deps": map[string]any{
									"skills": map[string]any{
										"use": "skill/filesystem",
										"config": map[string]any{
											"dirs": []string{dir},
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
	_, err = ag.RunTurn(context.Background(), agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "load skill"}},
		},
	})
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	replay, err := ag.Session().DeriveMessages(context.Background())
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
