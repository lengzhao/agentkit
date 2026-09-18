package sessbind_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestGlobalRuntimeUnifiedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	ctx := context.Background()

	if err := sessbind.SetGlobalAgentBind(ctx, ws, "worker"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalModelBind(ctx, ws, "worker", "gpt-4o"); err != nil {
		t.Fatal(err)
	}

	path, err := ws.Resolve(ctx, "global:runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "worker") || !strings.Contains(body, "gpt-4o") {
		t.Fatalf("global runtime = %s", body)
	}

	got, err := sessbind.GlobalAgentBind(ctx, ws)
	if err != nil || got != "worker" {
		t.Fatalf("agent = %q err=%v", got, err)
	}
	gotModel, err := sessbind.GlobalModelBind(ctx, ws, agentkit.AgentID("worker"))
	if err != nil || gotModel != "gpt-4o" {
		t.Fatalf("model = %q err=%v", gotModel, err)
	}
}
