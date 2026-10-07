//go:build unix

package shell

import (
	"context"
	"testing"
	"time"

	"github.com/lengzhao/agentkit/runtime/tooloutput"
	workspaceruntime "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestShellBashTimeoutKillsBackgroundChild(t *testing.T) {
	root := t.TempDir()
	ws, err := workspaceruntime.New(workspaceruntime.Config{Global: root, Local: root, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	ex := &bashExecutor{
		relWorkDir: ".",
		workspace:  ws,
		commands:   map[string][]string{},
		cfg:        ShellBashConfig{TimeoutSeconds: 2},
	}

	start := time.Now()
	out, err := ex.run(context.Background(), "sleep 120 & wait", nil)
	elapsed := time.Since(start)
	// Without process-group teardown, Run can block until the background sleep exits (~120s).
	if elapsed > 15*time.Second {
		t.Fatalf("timeout cleanup took %v; expected well under background sleep duration", elapsed)
	}
	if err != nil {
		if _, ok := tooloutput.AsTimeoutError(err); !ok {
			t.Fatalf("err = %v", err)
		}
		return
	}
	if out.ExitCode == 0 {
		t.Fatalf("expected non-zero exit after timeout kill, got %#v", out)
	}
}

func TestShellBashCancelKillsBackgroundChild(t *testing.T) {
	root := t.TempDir()
	ws, err := workspaceruntime.New(workspaceruntime.Config{Global: root, Local: root, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	ex := &bashExecutor{
		relWorkDir: ".",
		workspace:  ws,
		commands:   map[string][]string{},
		cfg:        ShellBashConfig{TimeoutSeconds: 60},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = ex.run(ctx, "sleep 120 & wait", nil)
	elapsed := time.Since(start)
	if elapsed > 15*time.Second {
		t.Fatalf("cancel cleanup took %v", elapsed)
	}
	if err == nil {
		t.Fatalf("expected cancel error, got nil")
	}
}
