package acpremote

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestResolveSessionMCPWithoutProviderIsEmptyArray(t *testing.T) {
	t.Parallel()

	b := newBridge(Config{}, nil, nil)
	got, err := b.resolveSessionMCP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice for session/new mcpServers")
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestResolveCwdDefaultsToWork(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws := rtworkspace.Static(root)
	b := newBridge(Config{}, ws, nil)

	cwd, err := b.resolveCwd(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "work")
	if cwd != want {
		t.Fatalf("cwd = %q, want %q", cwd, want)
	}
}

func TestResolveCwdExplicitOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws := rtworkspace.Static(root)
	b := newBridge(Config{Cwd: "work/agent-harness"}, ws, nil)

	cwd, err := b.resolveCwd(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "work", "agent-harness")
	if cwd != want {
		t.Fatalf("cwd = %q, want %q", cwd, want)
	}
}

func TestTerminateAndWaitSubprocessNoDeadlock(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(done)
	}()

	finished := make(chan struct{})
	go func() {
		terminateAndWaitSubprocess(&subprocess{done: done}, nil)
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("terminateAndWaitSubprocess deadlocked while holding bridge mutex")
	}
}

func TestReleaseSubprocessNoDeadlock(t *testing.T) {
	t.Parallel()

	b := newBridge(Config{}, nil, nil)
	done := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(done)
	}()

	b.mu.Lock()
	b.proc = &subprocess{done: done}
	b.mu.Unlock()

	finished := make(chan struct{})
	go func() {
		b.releaseSubprocess()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("releaseSubprocess deadlocked")
	}
}
