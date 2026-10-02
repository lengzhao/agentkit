package acpremote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// writeArgvProbe installs a shell wrapper that logs argv to logPath then exits 0.
func writeArgvProbe(t *testing.T) (wrapperPath, logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "argv.log")
	wrapperPath = filepath.Join(dir, "argv-probe.sh")
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true not in PATH")
	}
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\nexec %q\n", logPath, trueBin)
	if err := os.WriteFile(wrapperPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return wrapperPath, logPath
}

func TestConnectSubprocessSandboxWrapArgv(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := rtworkspace.Static(root)
	wantCwd := work

	inner := []string{"/acp/agent", "--test-flag"}
	wrapper, logPath := writeArgvProbe(t)
	rec := &agenttest.RecordingSandbox{
		WrappedArgv: append([]string{wrapper, "sandbox-marker"}, inner...),
	}

	cfg := Config{Command: slices.Clone(inner)}
	b := newBridge(cfg, ws, nil, nil, nil, rec)
	defer b.stop()

	_, err := b.ensureConn(context.Background())
	if err == nil {
		t.Fatal("expected ACP initialize failure with probe wrapper")
	}

	call, ok := rec.LastWrapCall()
	if !ok {
		t.Fatal("WrapArgv must be invoked before exec")
	}
	if call.Cwd != wantCwd {
		t.Fatalf("WrapArgv cwd = %q, want %q", call.Cwd, wantCwd)
	}
	if !slices.Equal(call.Inner, inner) {
		t.Fatalf("WrapArgv inner = %v, want %v", call.Inner, inner)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("wrapped argv must reach exec (log missing): %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if lines[0] != "sandbox-marker" {
		t.Fatalf("exec argv log = %q, want sandbox-marker first", string(raw))
	}
	if !slices.Equal(lines[1:], inner) {
		t.Fatalf("inner command not passed through wrap, log lines=%v inner=%v", lines[1:], inner)
	}
}

func TestConnectSubprocessSandboxWrapFailClosed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := rtworkspace.Static(root)
	_, logPath := writeArgvProbe(t)

	rec := &agenttest.RecordingSandbox{WrapErr: errors.New("sandbox unavailable")}
	cfg := Config{Command: []string{"/acp/agent"}}
	b := newBridge(cfg, ws, nil, nil, nil, rec)
	defer b.stop()

	_, err := b.ensureConn(context.Background())
	if err == nil || !strings.Contains(err.Error(), "acp sandbox wrap") {
		t.Fatalf("WrapArgv error must fail closed before start, got %v", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("subprocess must not start when WrapArgv fails")
	}
}

func TestNewNilSandboxDefaultsDisabled(t *testing.T) {
	rt, err := New(Config{
		ID:      "acp",
		Command: []string{"/nonexistent/acp"},
	}, Deps{
		Workspace: &stubWorkspace{},
		FS:        bindTestFS(t, t.TempDir()),
		Telemetry: mustToolkit(t),
		Sandbox:   nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := rt.(*Runtime)
	if agent.sandbox.Enabled() {
		t.Fatal("nil Sandbox dep must default to capsandbox.Disabled()")
	}
	inner := []string{"echo", "hi"}
	out, err := agent.sandbox.WrapArgv(context.Background(), "/tmp", inner)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out, inner) {
		t.Fatalf("Disabled sandbox must pass argv through, got %v", out)
	}
}
