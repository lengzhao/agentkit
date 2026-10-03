package setmodel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func newTool(t *testing.T, cfg Config) (agentkit.Tool, agentkit.SessionStore) {
	t.Helper()
	store, _ := agenttest.TempFileStore(t)
	tool, err := New(cfg, Deps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	return tool, store
}

func TestSetModelPersistsSessionOverride(t *testing.T) {
	tool, store := newTool(t, Config{})
	ctx := agenttest.TurnContext("s1", "agent-1")

	raw := agenttest.CallTool(t, ctx, tool, `{"model":"gpt-4o-mini"}`)
	var out Output
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if out.Model != "gpt-4o-mini" {
		t.Fatalf("out = %+v", out)
	}
	got, err := store.(agentkit.SessionRuntimeStore).ModelBind(ctx, "s1")
	if err != nil || got != "gpt-4o-mini" {
		t.Fatalf("bind = %q err=%v", got, err)
	}

	raw = agenttest.CallTool(t, ctx, tool, `{"model":"reset"}`)
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Reset {
		t.Fatalf("out = %+v", out)
	}
	got, err = store.(agentkit.SessionRuntimeStore).ModelBind(ctx, "s1")
	if err != nil || got != "" {
		t.Fatalf("after reset bind = %q err=%v", got, err)
	}
}

func TestSetModelAllowModels(t *testing.T) {
	tool, _ := newTool(t, Config{AllowModels: []string{"gpt-4o", "gpt-4o-mini"}})
	ctx := agenttest.TurnContext("s1", "agent-1")

	raw := agenttest.CallTool(t, ctx, tool, `{"model":"claude-sonnet-4"}`)
	if !strings.Contains(raw, "not in allowModels") {
		t.Fatalf("raw = %s", raw)
	}
	raw = agenttest.CallTool(t, ctx, tool, `{"model":"gpt-4o"}`)
	var out Output
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if out.Model != "gpt-4o" {
		t.Fatalf("out = %+v", out)
	}
}

func TestSetModelDeniedInSubagent(t *testing.T) {
	tool, _ := newTool(t, Config{})
	ctx := context.WithValue(agenttest.TurnContext("s1", "agent-1"), agentkit.KeyInSubagent, true)
	raw := agenttest.CallTool(t, ctx, tool, `{"model":"gpt-4o"}`)
	if !strings.Contains(raw, "subagent") {
		t.Fatalf("raw = %s", raw)
	}
}
