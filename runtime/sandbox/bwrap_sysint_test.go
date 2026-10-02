//go:build sysint

// System-integration tests: run the real bwrap and assert actual isolation
// (not just argv shape). Require Linux + bubblewrap in PATH + unprivileged
// user namespaces. Skip when bwrap is unavailable, unless SYSINT_REQUIRED=1
// (CI) — a silently skipped security test is worse than a loud failure.

package sandbox

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/testing/agenttest"
)

// sysintEnv builds a real Sandbox (mode=bwrap, probed) over a two-tenant
// workspace layout:
//
//	base/tenants/tenant-a, base/tenants/tenant-b, base/global
func sysintEnv(t *testing.T) (*Sandbox, string, string) {
	t.Helper()
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	localA := filepath.Join(base, "tenants", "tenant-a")
	localB := filepath.Join(base, "tenants", "tenant-b")
	global := filepath.Join(base, "global")
	for _, d := range []string{localA, localB, global} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(localB, "neighbor-secret.txt"), []byte("neighbor"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, "secrets.enc.json"), []byte("top-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{Mode: ModeBwrap, TmpBase: t.TempDir()}, Deps{Workspace: agenttest.ScopedWorkspace{Local: localA, Global: global}})
	if err != nil {
		if os.Getenv("SYSINT_REQUIRED") == "1" {
			t.Fatalf("bwrap required but unavailable: %v", err)
		}
		t.Skipf("bwrap unavailable: %v", err)
	}
	s := svc.(*Sandbox)
	if !s.Enabled() {
		t.Fatal("sandbox must be enabled in bwrap mode")
	}
	return s, localA, global
}

// runSandboxed executes cmd inside the sandbox and returns exit code and
// combined output.
func runSandboxed(t *testing.T, s *Sandbox, workDir, cmd string) (int, string) {
	t.Helper()
	argv, err := s.WrapArgv(context.Background(), workDir, []string{"bash", "-c", cmd})
	if err != nil {
		t.Fatalf("WrapArgv: %v", err)
	}
	c := exec.Command(argv[0], argv[1:]...)
	out, runErr := c.CombinedOutput()
	exit := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exit = exitErr.ExitCode()
		} else {
			t.Fatalf("run sandboxed command: %v", runErr)
		}
	}
	return exit, string(out)
}

// S1: neighbor tenants are invisible inside the sandbox.
func TestSysintNeighborTenantInvisible(t *testing.T) {
	s, localA, _ := sysintEnv(t)
	workDir := localA

	code, out := runSandboxed(t, s, workDir, "ls ..")
	if code == 0 && strings.Contains(out, "tenant-b") {
		t.Fatalf("neighbor tenant visible in parent listing: %q", out)
	}
	code, _ = runSandboxed(t, s, workDir, "cat ../tenant-b/neighbor-secret.txt")
	if code == 0 {
		t.Fatal("read of neighbor tenant file must fail")
	}
}

// S2: secretFiles under the global root are masked empty.
func TestSysintSecretFileMasked(t *testing.T) {
	s, localA, global := sysintEnv(t)
	code, out := runSandboxed(t, s, localA, "cat "+filepath.Join(global, "secrets.enc.json"))
	if code != 0 {
		t.Fatalf("reading masked secret should succeed with empty content, exit=%d out=%q", code, out)
	}
	if strings.Contains(out, "top-secret") {
		t.Fatalf("secret content leaked: %q", out)
	}
}

// S3: read-only floor — writes outside the tenant root fail, inside succeed.
func TestSysintReadOnlyFloor(t *testing.T) {
	s, localA, global := sysintEnv(t)
	if code, out := runSandboxed(t, s, localA, "echo x > "+filepath.Join(global, "deny.txt")); code == 0 {
		t.Fatalf("write to global root must fail (EROFS), out=%q", out)
	}
	if code, out := runSandboxed(t, s, localA, "echo x > "+filepath.Join(localA, "ok.txt")+" && cat "+filepath.Join(localA, "ok.txt")); code != 0 || !strings.Contains(out, "x") {
		t.Fatalf("write inside tenant root must succeed, exit=%d out=%q", code, out)
	}
	if code, _ := runSandboxed(t, s, localA, "echo x > /tmp/scratch.txt"); code != 0 {
		t.Fatal("write to /tmp must succeed")
	}
}

// S4: pid namespace — host processes are unreachable from the sandbox.
func TestSysintPidNamespaceIsolatesHostProcesses(t *testing.T) {
	s, localA, _ := sysintEnv(t)
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Skipf("cannot start host sleeper: %v", err)
	}
	defer sleeper.Process.Kill()
	defer sleeper.Wait()

	code, _ := runSandboxed(t, s, localA, fmt.Sprintf("kill -0 %d", sleeper.Process.Pid))
	if code == 0 {
		t.Fatal("host process must be unreachable (ESRCH) inside the pid namespace")
	}
}

// S5: network stack is preserved — a loopback listener on the host is
// reachable from inside the sandbox (no --unshare-net).
func TestSysintLoopbackPreserved(t *testing.T) {
	s, localA, _ := sysintEnv(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	code, out := runSandboxed(t, s, localA, fmt.Sprintf("exec 3<>/dev/tcp/127.0.0.1/%d", port))
	if code != 0 {
		t.Fatalf("loopback must be reachable inside the sandbox, exit=%d out=%q", code, out)
	}
}

// Guard the guard: when the tenant identity cannot be resolved, WrapArgv must
// fail closed (no silent passthrough of an unwrapped command).
func TestSysintWrapFailsClosedOnUnresolvableTenant(t *testing.T) {
	s, _, _ := sysintEnv(t)
	s.workspace = brokenWorkspace{}
	if _, err := s.WrapArgv(context.Background(), t.TempDir(), []string{"true"}); err == nil {
		t.Fatal("WrapArgv must fail when the tenant root cannot be resolved")
	}
}

type brokenWorkspace struct{}

func (brokenWorkspace) Resolve(context.Context, string) (string, error) {
	return "", fmt.Errorf("workspace unavailable")
}
