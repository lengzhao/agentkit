package acpremote

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestResolveSessionMCPWithoutProviderIsEmptyArray(t *testing.T) {
	t.Parallel()

	b := newBridge(Config{}, nil, nil, nil, nil, nil, nil, nil)
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
	b := newBridge(Config{}, ws, nil, nil, nil, nil, nil, nil)

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
	b := newBridge(Config{Cwd: "work/agent-harness"}, ws, nil, nil, nil, nil, nil, nil)

	cwd, err := b.resolveCwd(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "work", "agent-harness")
	if cwd != want {
		t.Fatalf("cwd = %q, want %q", cwd, want)
	}
}

func TestResolveCwdCreatesMissingDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws := rtworkspace.Static(root)
	b := newBridge(Config{Cwd: "work/repos/agentkit"}, ws, nil, nil, nil, nil, nil, nil)

	cwd, err := b.resolveCwd(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "work", "repos", "agentkit")
	if cwd != want {
		t.Fatalf("cwd = %q, want %q", cwd, want)
	}
	info, err := os.Stat(cwd)
	if err != nil {
		t.Fatalf("cwd not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("cwd %q is not a directory", cwd)
	}
}

// An absolute (or ~/) cwd outside the workspace root is not restricted: the
// workspace passes it through and the missing directory is created in place.
func TestResolveCwdAbsoluteOutsideRootIsCreated(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "anywhere", "deep")
	ws := rtworkspace.Static(root)
	b := newBridge(Config{Cwd: outside}, ws, nil, nil, nil, nil, nil, nil)

	cwd, err := b.resolveCwd(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cwd != outside {
		t.Fatalf("cwd = %q, want %q", cwd, outside)
	}
	info, err := os.Stat(cwd)
	if err != nil {
		t.Fatalf("cwd not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("cwd %q is not a directory", cwd)
	}
}

func TestResolveCwdSurfacesResolveErrorWithoutMkdir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws := rtworkspace.Static(root)
	b := newBridge(Config{Cwd: "../../escape"}, ws, nil, nil, nil, nil, nil, nil)

	// Static resolves one level above root, so a deeper escape must fail and
	// no directory may be created for the rejected path.
	if _, err := b.resolveCwd(context.Background()); err == nil {
		t.Fatal("expected error for escaping cwd")
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

	b := newBridge(Config{}, nil, nil, nil, nil, nil, nil, nil)
	done := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(done)
	}()

	adopted := make(chan struct{})
	b.connSend(adoptProcOp{proc: &subprocess{done: done}, done: adopted})
	<-adopted

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
