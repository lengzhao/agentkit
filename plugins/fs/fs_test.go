package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/fs"
)

func TestLocalBrowseOperations(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "util.go"), []byte("package pkg\nconst Version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc, err := fs.NewLocal(fs.LocalConfig{Root: "."}, fs.LocalDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	entries, err := svc.ListDir(ctx, ".")
	if err != nil {
		t.Fatalf("list dir: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}

	grepResult, err := svc.Grep(ctx, filesystem.GrepRequest{Pattern: "func main"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if len(grepResult.Matches) != 1 || grepResult.Matches[0].Path != "main.go" {
		t.Fatalf("unexpected grep matches: %+v", grepResult.Matches)
	}

	findResult, err := svc.Find(ctx, filesystem.FindRequest{Pattern: "*.go"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(findResult.Paths) != 2 {
		t.Fatalf("expected 2 go files, got %v", findResult.Paths)
	}
}

func TestReadonlyAllowsReadDeniesWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inner, err := fs.NewLocal(fs.LocalConfig{Root: "."}, fs.LocalDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatalf("local fs: %v", err)
	}
	if err := inner.WriteText(context.Background(), "a.txt", "hello"); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	ro, err := fs.NewReadonly(fs.ReadonlyConfig{}, fs.ReadonlyDeps{FS: inner})
	if err != nil {
		t.Fatalf("readonly fs: %v", err)
	}

	text, err := ro.ReadText(context.Background(), "a.txt", 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if text != "hello" {
		t.Fatalf("read text = %q", text)
	}
	if err := ro.WriteText(context.Background(), "b.txt", "nope"); err == nil {
		t.Fatal("expected write error")
	} else if !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("unexpected write error: %v", err)
	}
	_, err = ro.Edit(context.Background(), filesystem.EditRequest{
		Path:      "a.txt",
		OldString: "hello",
		NewString: "world",
	})
	if err == nil {
		t.Fatal("expected edit error")
	}
}
