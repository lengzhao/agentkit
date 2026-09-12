package learning

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestStagedMemoryApproveReject(t *testing.T) {
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approve := true
	svc := &Service{
		memoryRoot: ".",
		review:     ReviewServiceConfig{WriteApproval: &approve},
		workspace:  ws,
	}
	ctx := context.Background()
	out, err := svc.CaptureMemoryAdd(ctx, "prefers tea", "background-review")
	if err != nil {
		t.Fatal(err)
	}
	if out == "" || !contains(out, "staged") {
		t.Fatalf("expected staged, got %q", out)
	}
	stagedPath := filepath.Join(dir, "memory", ".staged", "pending.json")
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("staged file: %v", err)
	}
	memPath := filepath.Join(dir, "memory.md")
	if _, err := os.Stat(memPath); err == nil {
		t.Fatal("memory.md should not exist before approve")
	}
	pending, err := svc.listPending(ctx)
	if err != nil || !contains(pending, "staged memory") {
		t.Fatalf("pending: %q err=%v", pending, err)
	}
	entries, _ := svc.stagedStore(ctx)
	list, _ := entries.List()
	if len(list) != 1 {
		t.Fatalf("staged count = %d", len(list))
	}
	id := list[0].ID
	if _, err := svc.approveStaged(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(memPath); err != nil {
		t.Fatalf("memory.md after approve: %v", err)
	}
}

func TestStagedMemoryReject(t *testing.T) {
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approve := true
	svc := &Service{memoryRoot: ".", review: ReviewServiceConfig{WriteApproval: &approve}, workspace: ws}
	ctx := context.Background()
	_, err = svc.CaptureMemoryAdd(ctx, "discard me", "background-review")
	if err != nil {
		t.Fatal(err)
	}
	store, _ := svc.stagedStore(ctx)
	list, _ := store.List()
	if _, err := svc.rejectStaged(ctx, list[0].ID); err != nil {
		t.Fatal(err)
	}
	list, _ = store.List()
	if len(list) != 0 {
		t.Fatalf("expected empty staged, got %d", len(list))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
