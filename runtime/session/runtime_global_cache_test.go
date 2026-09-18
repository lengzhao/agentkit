package session_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/runtime/session"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestGlobalRuntimeLoadUsesCacheUntilSave(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	ctx := context.Background()

	if err := session.SetGlobalAgentBind(ctx, ws, "worker"); err != nil {
		t.Fatal(err)
	}
	got, err := session.GlobalAgentBind(ctx, ws)
	if err != nil || got != "worker" {
		t.Fatalf("first read agent = %q err=%v", got, err)
	}
	got, err = session.GlobalAgentBind(ctx, ws)
	if err != nil || got != "worker" {
		t.Fatalf("cached read agent = %q err=%v", got, err)
	}

	if err := session.SetGlobalAgentBind(ctx, ws, "reviewer"); err != nil {
		t.Fatal(err)
	}
	got, err = session.GlobalAgentBind(ctx, ws)
	if err != nil || got != "reviewer" {
		t.Fatalf("after save agent = %q err=%v", got, err)
	}
}
