package sessstore_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestStoreModelBindFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{
		Workspace: rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeStore, ok := store.(agentkit.SessionRuntimeStore)
	if !ok {
		t.Fatal("expected SessionRuntimeStore")
	}

	ctx := context.Background()
	sessionID := agentkit.SessionID("model-bind-test")

	if got, err := runtimeStore.ModelBind(ctx, sessionID); err != nil || got != "" {
		t.Fatalf("initial bind = %q err=%v", got, err)
	}
	if err := runtimeStore.SetModelBind(ctx, sessionID, "claude-sonnet-4"); err != nil {
		t.Fatal(err)
	}
	if got, err := runtimeStore.ModelBind(ctx, sessionID); err != nil || got != "claude-sonnet-4" {
		t.Fatalf("bind = %q err=%v", got, err)
	}

	path := filepath.Join(dir, "model-bind-test", "runtime.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("runtime.json missing: %v", err)
	}
	if !strings.Contains(string(raw), "claude-sonnet-4") {
		t.Fatalf("runtime.json = %s", raw)
	}

	reopened, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{
		Workspace: rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	reopenedRuntime, ok := reopened.(agentkit.SessionRuntimeStore)
	if !ok {
		t.Fatal("expected SessionRuntimeStore")
	}
	if got, err := reopenedRuntime.ModelBind(ctx, sessionID); err != nil || got != "claude-sonnet-4" {
		t.Fatalf("reopened bind = %q err=%v", got, err)
	}

	if err := reopenedRuntime.SetModelBind(ctx, sessionID, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := reopenedRuntime.ModelBind(ctx, sessionID); err != nil || got != "" {
		t.Fatalf("cleared bind = %q err=%v", got, err)
	}
}
