package sessstore_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestGlobalRuntimeLoadUsesCacheUntilSave(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	ctx := context.Background()

	if err := sessstore.SetGlobalAgentBind(ctx, ws, "worker"); err != nil {
		t.Fatal(err)
	}
	got, err := sessstore.GlobalAgentBind(ctx, ws)
	if err != nil || got != "worker" {
		t.Fatalf("first read agent = %q err=%v", got, err)
	}
	got, err = sessstore.GlobalAgentBind(ctx, ws)
	if err != nil || got != "worker" {
		t.Fatalf("cached read agent = %q err=%v", got, err)
	}

	if err := sessstore.SetGlobalAgentBind(ctx, ws, "reviewer"); err != nil {
		t.Fatal(err)
	}
	got, err = sessstore.GlobalAgentBind(ctx, ws)
	if err != nil || got != "reviewer" {
		t.Fatalf("after save agent = %q err=%v", got, err)
	}
}
