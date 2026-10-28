package fs

import (
	"context"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"os"
	"path/filepath"
	"testing"
)

func TestFileToolsUseTurnCwd(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	ws := rtworkspace.Static(root)
	store, err := rtfilesystem.New(rtfilesystem.Config{Root: ".", Unrestricted: true}, rtfilesystem.Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "project.txt"), []byte("project data"), 0600); err != nil {
		t.Fatal(err)
	}
	a := fsAdapter{store: store, workspace: ws}
	ctx := workspace.WithWorkingDirectory(context.Background(), project)
	text, err := a.readText(ctx, "project.txt", 0)
	if err != nil || text != "project data" {
		t.Fatalf("read %q %v", text, err)
	}
	entries, err := a.listDir(ctx, "")
	if err != nil || len(entries) != 1 || entries[0].Name != "project.txt" {
		t.Fatalf("list: %+v %v", entries, err)
	}
}
