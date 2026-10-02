package agenttest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/workspace"
)

// ScopedWorkspace resolves local: and global: refs the way sandbox/bwrap tests
// expect (tenant root under Local, shared state under Global).
type ScopedWorkspace struct {
	Local  string
	Global string
}

func (w ScopedWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	if strings.HasPrefix(rel, "global:") {
		return filepath.Join(w.Global, strings.TrimPrefix(rel, "global:")), nil
	}
	return filepath.Join(w.Local, strings.TrimPrefix(rel, "local:")), nil
}

var _ workspace.Service = ScopedWorkspace{}

// SetupScopedTenantDirs creates .../tenants/tenant-a and .../global under a
// realpath-normalized temp dir and writes secrets.enc.json in global.
func SetupScopedTenantDirs(t *testing.T) (ws ScopedWorkspace, local, global string) {
	t.Helper()
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	local = filepath.Join(base, "tenants", "tenant-a")
	global = filepath.Join(base, "global")
	for _, d := range []string{local, global} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(global, "secrets.enc.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return ScopedWorkspace{Local: local, Global: global}, local, global
}
