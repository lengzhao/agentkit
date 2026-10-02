package sandbox

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
)

// In-process CheckRead/Write and subprocess WrapArgv must agree on what is
// visible or masked in the tenant view.
func TestSubprocessAndInProcessViewAlignment(t *testing.T) {
	s, local, global := newTestSandbox(t)
	ctx := context.Background()
	workDir := filepath.Join(local, "work")

	secret := filepath.Join(global, "secrets.enc.json")
	if err := s.CheckRead(ctx, secret); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("secret must be denied in-process, got %v", err)
	}

	neighbor := filepath.Join(filepath.Dir(local), "tenant-b", "work", "x")
	if err := s.CheckRead(ctx, neighbor); !errors.Is(err, capsandbox.ErrDenied) {
		t.Fatalf("neighbor must be denied in-process, got %v", err)
	}

	if err := s.CheckRead(ctx, filepath.Join(local, "work", "main.go")); err != nil {
		t.Fatalf("tenant path must be allowed in-process: %v", err)
	}

	argv, err := s.WrapArgv(ctx, workDir, []string{"bash", "-lc", "true"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, "\x00")

	if !strings.Contains(joined, "/dev/null\x00"+secret) {
		t.Fatalf("subprocess argv must mask secret denied in-process, argv=%v", argv)
	}
	tenantsBase := filepath.Dir(local)
	if mountIndex(argv, "--tmpfs", tenantsBase) < 0 {
		t.Fatalf("subprocess must tmpfs-mask neighbor tenants, argv=%v", argv)
	}
	if !slices.Contains(argv, "--unshare-pid") {
		t.Fatalf("subprocess isolation must include pid namespace, argv=%v", argv)
	}
}
