package memory

import (
	"context"
	"testing"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type countingObserver struct {
	n int
}

func (c *countingObserver) OnMemoryCommitted(context.Context, string, string, capmemory.AddOutcome) {
	c.n++
}

func TestRegisterCommitObserverMultiple(t *testing.T) {
	t.Parallel()

	svc, err := New(Config{}, Deps{FS: testFS(t, rtworkspace.Static(t.TempDir()))})
	if err != nil {
		t.Fatal(err)
	}
	a, b := &countingObserver{}, &countingObserver{}
	svc.RegisterCommitObserver(a)
	svc.RegisterCommitObserver(b)
	svc.notifyCommitted(context.Background(), "x", "test", capmemory.AddOutcomeAdded)
	if a.n != 1 || b.n != 1 {
		t.Fatalf("a=%d b=%d", a.n, b.n)
	}
}

func TestRemoveMemoryNotifiesObservers(t *testing.T) {
	t.Parallel()

	svc, err := New(Config{}, Deps{FS: testFS(t, rtworkspace.Static(t.TempDir()))})
	if err != nil {
		t.Fatal(err)
	}
	obs := &countingObserver{}
	svc.RegisterCommitObserver(obs)
	ctx := context.Background()
	if _, err := svc.addMemory(ctx, "fact to drop", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.removeMemory(ctx, "drop", "test"); err != nil {
		t.Fatal(err)
	}
	if obs.n != 2 {
		t.Fatalf("expected add+remove notifications, got %d", obs.n)
	}
}
