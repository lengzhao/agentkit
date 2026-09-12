package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	rw "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestWalkLocalTenantsDiscoversDirs(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	global := t.TempDir()
	for _, name := range []string{"slack_C001", "slack_C002"} {
		if err := os.MkdirAll(filepath.Join(base, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	svc := newTenantSvc(t, rw.TenantConfig{Global: global, LocalBase: base})
	var n int
	if err := svc.WalkLocalTenants(context.Background(), func(context.Context) error {
		n++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("visited %d tenants, want 2", n)
	}
}
