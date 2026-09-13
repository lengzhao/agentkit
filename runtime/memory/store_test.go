package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
)

func TestMemoryStoreAddAndLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "memory.md")
	store := NewMemoryStore(path, 200)
	res, err := store.Add("likes tea")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != capmemory.AddOutcomeAdded {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	entries, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Content != "likes tea" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestMemoryStoreRejectsSecret(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(t.TempDir()+"/memory.md", 200)
	_, err := store.Add("token sk-abcdefghijklmnopqrstuvwxyz")
	if err == nil {
		t.Fatal("expected secret rejection")
	}
}

func TestMemoryStoreSkipsDuplicate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "memory.md")
	store := NewMemoryStore(path, 200)
	if _, err := store.Add("fact a"); err != nil {
		t.Fatal(err)
	}
	res, err := store.Add("fact a")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != capmemory.AddOutcomeDuplicate {
		t.Fatalf("outcome = %q", res.Outcome)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "fact a") != 1 {
		t.Fatalf("body = %q", data)
	}
}
