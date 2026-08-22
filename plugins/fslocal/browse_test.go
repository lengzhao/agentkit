package fslocal_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/plugins/fslocal"
)

func TestBrowseOperations(t *testing.T) {
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

	fs, err := fslocal.New(fslocal.Config{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	entries, err := fs.ListDir(ctx, ".")
	if err != nil {
		t.Fatalf("list dir: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}

	grepResult, err := fs.Grep(ctx, filesystem.GrepRequest{Pattern: "func main"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if len(grepResult.Matches) != 1 || grepResult.Matches[0].Path != "main.go" {
		t.Fatalf("unexpected grep matches: %+v", grepResult.Matches)
	}

	findResult, err := fs.Find(ctx, filesystem.FindRequest{Pattern: "*.go"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(findResult.Paths) != 2 {
		t.Fatalf("expected 2 go files, got %v", findResult.Paths)
	}
}
