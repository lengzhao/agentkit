package shell_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	capshell "github.com/lengzhao/agentkit/cap/shell"
	"github.com/lengzhao/agentkit/plugins/tool/shell"
	"github.com/lengzhao/agentkit/runtime/tooloutput"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func newExecutor(t *testing.T, cfg shell.ShellBashConfig, dir string) capshell.Executor {
	t.Helper()
	ex, err := shell.NewShellBash(cfg, shell.ShellBashDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	impl, ok := ex.(capshell.Executor)
	if !ok {
		t.Fatalf("New returned %T", ex)
	}
	return impl
}

func TestRunCapturesOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ex := newExecutor(t, shell.ShellBashConfig{WorkDir: "."}, dir)
	res, err := ex.Run(context.Background(), capshell.Request{Command: "echo hello"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
	if strings.TrimSpace(res.Stdout) != "hello" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

// A non-zero exit is a result, not an error: the cron helper decides whether to
// treat it as a failed job.
func TestRunReportsNonZeroExitAsResult(t *testing.T) {
	t.Parallel()

	ex := newExecutor(t, shell.ShellBashConfig{}, t.TempDir())
	res, err := ex.Run(context.Background(), capshell.Request{Command: "exit 7"})
	if err != nil {
		t.Fatalf("non-zero exit must not be an error: %v", err)
	}
	if res.ExitCode != 7 {
		t.Fatalf("exit = %d, want 7", res.ExitCode)
	}
}

func TestRunHonorsRequestWorkDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	ex := newExecutor(t, shell.ShellBashConfig{WorkDir: "."}, dir)
	res, err := ex.Run(context.Background(), capshell.Request{Command: "pwd", WorkDir: "sub"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(res.Stdout), "sub") {
		t.Fatalf("pwd = %q, want the sub directory", res.Stdout)
	}
}

func TestRunTimesOut(t *testing.T) {
	t.Parallel()

	ex := newExecutor(t, shell.ShellBashConfig{TimeoutSeconds: 1}, t.TempDir())
	start := time.Now()
	_, err := ex.Run(context.Background(), capshell.Request{Command: "sleep 10"})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if _, ok := tooloutput.AsTimeoutError(err); !ok {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("timeout was not enforced, elapsed %s", elapsed)
	}
}

func TestRunRejectsEmptyCommand(t *testing.T) {
	t.Parallel()

	ex := newExecutor(t, shell.ShellBashConfig{}, t.TempDir())
	if _, err := ex.Run(context.Background(), capshell.Request{}); err == nil {
		t.Fatal("expected an error for an empty command")
	}
}

// TrimEnv keeps an operator's shell out of a scheduled job.
func TestTrimEnvDropsInheritedVariables(t *testing.T) {
	t.Setenv("SCHEDULED_ONLY_MARKER", "leaked")

	ex := newExecutor(t, shell.ShellBashConfig{TrimEnv: boolPointer(true)}, t.TempDir())
	res, err := ex.Run(context.Background(), capshell.Request{
		Command: `printf '%s' "${SCHEDULED_ONLY_MARKER:-absent}"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Stdout) != "absent" {
		t.Fatalf("inherited variable leaked through TrimEnv: %q", res.Stdout)
	}
}

func TestInheritedEnvIsKeptByDefault(t *testing.T) {
	t.Setenv("INTERACTIVE_MARKER", "present")

	ex := newExecutor(t, shell.ShellBashConfig{}, t.TempDir())
	res, err := ex.Run(context.Background(), capshell.Request{
		Command: `printf '%s' "${INTERACTIVE_MARKER:-absent}"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Stdout) != "present" {
		t.Fatalf("default env should inherit the daemon environment, got %q", res.Stdout)
	}
}

func TestExtraEnvReachesChild(t *testing.T) {
	t.Parallel()

	ex := newExecutor(t, shell.ShellBashConfig{TrimEnv: boolPointer(true), ExtraEnv: map[string]string{"JOB_TAG": "nightly"}}, t.TempDir())
	res, err := ex.Run(context.Background(), capshell.Request{
		Command: `printf '%s' "${JOB_TAG:-missing}"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Stdout) != "nightly" {
		t.Fatalf("extraEnv missing: %q", res.Stdout)
	}
}

func TestNewRequiresWorkspace(t *testing.T) {
	t.Parallel()

	if _, err := shell.NewShellBash(shell.ShellBashConfig{}, shell.ShellBashDeps{}); err == nil {
		t.Fatal("expected an error without a workspace")
	}
}

func boolPointer(v bool) *bool { return &v }

func TestRunHonorsRequestTimeoutAndCancellation(t *testing.T) {
	ex := newExecutor(t, shell.ShellBashConfig{TimeoutSeconds: 60}, t.TempDir())
	_, err := ex.Run(context.Background(), capshell.Request{Command: "sleep 10", Timeout: 50 * time.Millisecond})
	if _, ok := tooloutput.AsTimeoutError(err); !ok {
		t.Fatalf("request timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ex.Run(ctx, capshell.Request{Command: "sleep 10"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
