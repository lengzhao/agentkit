package smoke_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestSmokeWebScriptedSearchAndFetch(t *testing.T) {
	t.Parallel()

	graph := map[string]any{
		"search": map[string]any{
			"use": "tool/web-search-scripted",
			"config": map[string]any{
				"byQuery": map[string]any{
					"loop": []any{
						map[string]any{
							"title":   "Loop 并发模型",
							"url":     "https://example.com/docs/loop",
							"snippet": "同一 session 的 turn 串行执行",
						},
					},
				},
			},
		},
		"fetch": map[string]any{
			"use": "tool/web-fetch-scripted",
			"config": map[string]any{
				"pages": map[string]string{
					"/docs/loop": "<html><body><p>loop/default 按 SessionID 取锁，同一 session 的 turn 串行执行。</p></body></html>",
				},
			},
		},
	}

	search := agenttest.Build[agentkit.Tool](t, graph, "search")
	fetch := agenttest.Build[agentkit.Tool](t, graph, "fetch")
	ctx := context.Background()

	searchOut := agenttest.CallTool(t, ctx, search, `{"query":"loop 串行"}`)
	if !strings.Contains(searchOut, "Loop 并发模型") {
		t.Fatalf("search output = %s", searchOut)
	}

	fetchOut := agenttest.CallTool(t, ctx, fetch, `{"url":"https://example.com/docs/loop"}`)
	if !strings.Contains(fetchOut, "串行执行") {
		t.Fatalf("fetch output = %s", fetchOut)
	}
	if strings.Contains(fetchOut, "<html") {
		t.Fatalf("fetch should strip html tags, got raw markup")
	}

	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(fetchOut), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Content == "" {
		t.Fatal("fetch content empty")
	}
}
