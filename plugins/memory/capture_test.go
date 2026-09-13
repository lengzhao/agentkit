package memory

import (
	"context"
	"testing"

	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestCaptureMemoryAddStagesWhenApprovePolicy(t *testing.T) {
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approve := true
	mem, err := New(Config{Review: ReviewConfig{WriteApproval: &approve}}, Deps{Workspace: ws})
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
	if len(staged) != 1 || staged[0].Content != "likes integration tests" {
		t.Fatalf("staged = %+v", staged)
	}
}
