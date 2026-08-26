package agentkit_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
)

func TestGrepFindListDirTools(t *testing.T) {
	t.Parallel()

	graph := map[string]any{
		"tools": map[string]any{
			"use": "tools/runtime",
			"deps": map[string]any{
				"tools": []any{
					map[string]any{
						"use": "tool/fs-memory",
						"config": map[string]any{
							"files": map[string]string{
								"main.go":     "package main\nfunc main() {}\n",
								"pkg/util.go": "package pkg\n",
							},
							"tools": []string{"grep", "find", "ls"},
						},
					},
				},
				"approval": map[string]any{"use": "approval/auto-deny"},
			},
		},
	}

	rt, _, err := build.Build[agentkit.ToolRuntime](context.Background(), graph, "tools")
	if err != nil {
		t.Fatalf("build tools runtime: %v", err)
	}

	grepResult, err := rt.Execute(context.Background(), agentkit.ToolCall{
		ID:    "1",
		Name:  "grep",
		Input: json.RawMessage(`{"pattern":"func main"}`),
	})
	if err != nil {
		t.Fatalf("grep call: %v", err)
	}
	if len(grepResult.Content) == 0 || !strings.Contains(grepResult.Content[0].Text, "main.go") {
		t.Fatalf("unexpected grep result: %+v", grepResult)
	}

	findResult, err := rt.Execute(context.Background(), agentkit.ToolCall{
		ID:    "2",
		Name:  "find",
		Input: json.RawMessage(`{"pattern":"*.go"}`),
	})
	if err != nil {
		t.Fatalf("find call: %v", err)
	}
	if len(findResult.Content) == 0 || !strings.Contains(findResult.Content[0].Text, "main.go") {
		t.Fatalf("unexpected find result: %+v", findResult)
	}

	lsResult, err := rt.Execute(context.Background(), agentkit.ToolCall{
		ID:    "3",
		Name:  "ls",
		Input: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("ls call: %v", err)
	}
	if len(lsResult.Content) == 0 || !strings.Contains(lsResult.Content[0].Text, "main.go") {
		t.Fatalf("unexpected ls result: %+v", lsResult)
	}
}
