package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestStoreRuntimeUnifiedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{
		Workspace: rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sessionID := agentkit.SessionID("runtime-unified")

	runtimeStore := store.(agentkit.SessionRuntimeStore)

	if err := runtimeStore.SetAgentBind(ctx, sessionID, "reviewer"); err != nil {
		t.Fatal(err)
	}
	if err := runtimeStore.SetModelBind(ctx, sessionID, "gpt-4o"); err != nil {
		t.Fatal(err)
	}

	runtimePath := filepath.Join(dir, "runtime-unified", "runtime.json")
	raw, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "reviewer") || !strings.Contains(body, "gpt-4o") {
		t.Fatalf("runtime.json = %s", body)
	}
}
