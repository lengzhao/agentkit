package memory

import (
	"context"
	"strings"
	"testing"

	rtmem "github.com/lengzhao/agentkit/runtime/memory"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestCaptureMemoryAddStagesWhenApprovePolicy(t *testing.T) {
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approve := true
	mem, err := New(Config{Review: ReviewConfig{WriteApproval: &approve}}, Deps{FS: testFS(t, ws)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	msg, err := mem.CaptureMemoryAdd(ctx, "likes integration tests", "background-review")
	if err != nil {
		t.Fatal(err)
	}
	if msg == "" {
		t.Fatal("expected staged message")
	}
	staged, err := mem.ListStaged(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || staged[0].Action != rtmem.StagedActionAdd || staged[0].Content != "likes integration tests" {
		t.Fatalf("staged = %+v", staged)
	}
}

func TestCaptureMemoryRemoveStagesWhenApprovePolicy(t *testing.T) {
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approve := true
	mem, err := New(Config{Review: ReviewConfig{WriteApproval: &approve}}, Deps{FS: testFS(t, ws)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := mem.addMemory(ctx, "stale fact", "memory-cmd"); err != nil {
		t.Fatal(err)
	}
	msg, err := mem.CaptureMemoryRemove(ctx, "stale")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "staged") {
		t.Fatalf("msg = %q", msg)
	}
	staged, err := mem.ListStaged(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || staged[0].Action != rtmem.StagedActionRemove || staged[0].OldText != "stale" {
		t.Fatalf("staged = %+v", staged)
	}
	entries, _, _, err := mem.LoadEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("memory should be unchanged until approve: %+v", entries)
	}
}

func TestApproveStagedRemoveAndReplace(t *testing.T) {
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := New(Config{}, Deps{FS: testFS(t, ws)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := mem.addMemory(ctx, "likes tea", "memory-cmd"); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.stageMemory(ctx, rtmem.StagedActionRemove, "tea", "", "background-review"); err != nil {
		t.Fatal(err)
	}
	staged, _ := mem.ListStaged(ctx)
	if len(staged) != 1 {
		t.Fatal(staged)
	}
	if _, err := mem.ApproveStaged(ctx, staged[0].ID); err != nil {
		t.Fatal(err)
	}
	entries, _, _, err := mem.LoadEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty memory after staged remove approve: %+v", entries)
	}

	if _, err := mem.addMemory(ctx, "old habit", "memory-cmd"); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.stageMemory(ctx, rtmem.StagedActionReplace, "old", "new habit", "background-review"); err != nil {
		t.Fatal(err)
	}
	staged, _ = mem.ListStaged(ctx)
	if _, err := mem.ApproveStaged(ctx, staged[0].ID); err != nil {
		t.Fatal(err)
	}
	entries, _, _, err = mem.LoadEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Content != "new habit" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestApproveLegacyEncodedStagedAdd(t *testing.T) {
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := New(Config{}, Deps{FS: testFS(t, ws)})
	if err != nil {
		t.Fatal(err)
	}
	store, err := mem.stagedStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(context.Background(), rtmem.StagedMemory{
		ID:      "legacy-1",
		Content: "legacy add fact",
		Source:  "background-review",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.ApproveStaged(context.Background(), "legacy-1"); err != nil {
		t.Fatal(err)
	}
	entries, _, _, err := mem.LoadEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Content != "legacy add fact" {
		t.Fatalf("entries = %+v", entries)
	}
}
