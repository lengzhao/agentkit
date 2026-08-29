package filesystem_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

func TestIgnoreMatcherRootGitignore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := filesystem.LoadIgnoreMatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Ignored("ignored.txt", false) {
		t.Fatal("expected ignored.txt to be ignored")
	}
	if m.Ignored("keep.txt", false) {
		t.Fatal("expected keep.txt to be visible")
	}
	if !m.Ignored(".git/config", false) {
		t.Fatal("expected .git to be ignored")
	}
}

func TestIgnoreMatcherUploadVisible(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m, err := filesystem.LoadIgnoreMatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Ignored(".agent/sessions/foo.jsonl", false) {
		t.Fatal(".agent runtime paths should remain searchable")
	}
	if m.Ignored("upload/hello.go", false) {
		t.Fatal("inbound uploads should be searchable")
	}
}
