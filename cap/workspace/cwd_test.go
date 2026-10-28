package workspace_test

import (
	"context"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"path/filepath"
	"testing"
)

func TestExecutionDirectoryDoesNotMoveWorkspace(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	ws := rtworkspace.Static(root)
	ctx := workspace.WithWorkingDirectory(context.Background(), project)
	got, err := workspace.ResolveWorkingDirectory(ctx, ws, "work")
	if err != nil || got != project {
		t.Fatalf("%q %v", got, err)
	}
	state, err := ws.Resolve(ctx, "state")
	if err != nil || state != filepath.Join(root, "state") {
		t.Fatalf("workspace moved: %q %v", state, err)
	}
}
