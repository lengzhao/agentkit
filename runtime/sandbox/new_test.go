package sandbox

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/testing/agenttest"
)

func bwrapProbeOK(t *testing.T) bool {
	t.Helper()
	_, _, err := probeBwrap()
	return err == nil
}

func newSandboxDeps(t *testing.T) Deps {
	t.Helper()
	base := t.TempDir()
	return Deps{Workspace: agenttest.ScopedWorkspace{
		Local:  filepath.Join(base, "tenants", "a"),
		Global: filepath.Join(base, "global"),
	}}
}

func TestNewRequiresWorkspace(t *testing.T) {
	_, err := New(Config{Mode: ModeOff}, Deps{})
	if err == nil {
		t.Fatal("expected error when workspace is nil")
	}
}

func TestNewModeMatrix(t *testing.T) {
	probeOK := bwrapProbeOK(t)
	_, lookErr := exec.LookPath("bwrap")
	hasBwrapBinary := lookErr == nil

	t.Run("off", func(t *testing.T) {
		s, err := New(Config{Mode: ModeOff}, newSandboxDeps(t))
		if err != nil {
			t.Fatal(err)
		}
		if s.Enabled() {
			t.Fatal("mode=off must stay disabled")
		}
	})

	t.Run("auto_empty_env", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "tmp")
		s, err := New(Config{Mode: ModeAuto, Env: "", TmpBase: tmp}, newSandboxDeps(t))
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Enabled(); got != probeOK {
			t.Fatalf("auto without prod env: Enabled=%v, probeOK=%v (bwrap in PATH=%v)", got, probeOK, hasBwrapBinary)
		}
	})

	t.Run("auto_prod", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "tmp")
		s, err := New(Config{Mode: ModeAuto, Env: "production", TmpBase: tmp}, newSandboxDeps(t))
		if probeOK {
			if err != nil {
				t.Fatalf("prod auto with working bwrap must construct: %v", err)
			}
			if !s.Enabled() {
				t.Fatal("prod auto must enable sandbox when bwrap probe succeeds")
			}
			return
		}
		if err == nil {
			t.Fatal("prod auto upgrades to bwrap and must fail closed when probe fails")
		}
	})

	t.Run("bwrap_fail_closed", func(t *testing.T) {
		if probeOK {
			t.Skip("probe succeeds on this host")
		}
		tmp := filepath.Join(t.TempDir(), "tmp")
		_, err := New(Config{Mode: ModeBwrap, TmpBase: tmp}, newSandboxDeps(t))
		if err == nil {
			t.Fatal("mode=bwrap with unavailable deps must error (default failIfUnavailable)")
		}
	})

	t.Run("bwrap_fail_if_unavailable_false", func(t *testing.T) {
		if probeOK {
			t.Skip("probe succeeds on this host")
		}
		tmp := filepath.Join(t.TempDir(), "tmp")
		failFalse := false
		s, err := New(Config{Mode: ModeBwrap, TmpBase: tmp, FailIfUnavailable: &failFalse}, newSandboxDeps(t))
		if err != nil {
			t.Fatalf("failIfUnavailable=false must degrade without error: %v", err)
		}
		if s.Enabled() {
			t.Fatal("degraded bwrap must report disabled")
		}
	})

	t.Run("auto_degrades_when_probe_fails", func(t *testing.T) {
		if probeOK {
			t.Skip("probe succeeds on this host")
		}
		tmp := filepath.Join(t.TempDir(), "tmp")
		s, err := New(Config{Mode: ModeAuto, Env: "dev", TmpBase: tmp}, newSandboxDeps(t))
		if err != nil {
			t.Fatal(err)
		}
		if s.Enabled() {
			t.Fatal("non-prod auto must degrade to disabled when bwrap unavailable")
		}
	})
}
