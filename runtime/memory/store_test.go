package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// testStore builds a MemoryStore on a local filesystem rooted at dir.
func testStore(t *testing.T, path string, charLimit int) (*MemoryStore, string) {
	t.Helper()
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Global: dir, Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: "."}, rtfilesystem.Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	return NewMemoryStore(fs, path, charLimit), dir
}

func TestMemoryStoreAddAndLoad(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t, "memory.md", 200)
	ctx := context.Background()
	res, err := store.Add(ctx, "likes tea")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != capmemory.AddOutcomeAdded {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	entries, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Content != "likes tea" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestMemoryStoreRejectsSecret(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t, "memory.md", 200)
	_, err := store.Add(context.Background(), "token sk-abcdefghijklmnopqrstuvwxyz")
	if err == nil {
		t.Fatal("expected secret rejection")
	}
}

func TestMemoryStoreSkipsDuplicate(t *testing.T) {
	t.Parallel()
	store, dir := testStore(t, "memory.md", 200)
	ctx := context.Background()
	if _, err := store.Add(ctx, "fact a"); err != nil {
		t.Fatal(err)
	}
	res, err := store.Add(ctx, "fact a")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != capmemory.AddOutcomeDuplicate {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	data, err := os.ReadFile(filepath.Join(dir, "memory.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "fact a") != 1 {
		t.Fatalf("body = %q", data)
	}
}
