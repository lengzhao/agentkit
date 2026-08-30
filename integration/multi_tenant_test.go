//go:build integration

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	rw "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// E2E-511: workspace/tenant isolates tool work directories per channel key.
func TestIntegrationMultiTenantWorkDirIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("integration multi-tenant")
	}

	base := t.TempDir()
	global := t.TempDir()
	svc, err := rw.NewTenant(rw.TenantConfig{
		Global:    global,
		LocalBase: filepath.Join(base, "tenants"),
		Scope:     "local",
	})
	if err != nil {
		t.Fatal(err)
	}

	pack, err := fs.NewFSWorkspace(fs.FSWorkspaceConfig{
		Root:  "work",
		Tools: []string{"write"},
	}, fs.FSWorkspaceDeps{Workspace: svc})
	if err != nil {
		t.Fatal(err)
	}
	if len(pack) != 1 {
		t.Fatalf("tools = %d, want write only", len(pack))
	}
	writeTool := pack[0]

	ctxA := tenantCtx("slack:C001")
	ctxB := tenantCtx("slack:C002")

	agenttest.CallTool(t, ctxA, writeTool, `{"path":"marker.txt","content":"tenant-a"}`)
	agenttest.CallTool(t, ctxB, writeTool, `{"path":"marker.txt","content":"tenant-b"}`)

	pathA, err := svc.Resolve(ctxA, "work/marker.txt")
	if err != nil {
		t.Fatal(err)
	}
	pathB, err := svc.Resolve(ctxB, "work/marker.txt")
	if err != nil {
		t.Fatal(err)
	}
	if pathA == pathB {
		t.Fatalf("both tenants share work path: %q", pathA)
	}

	rawA, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatal(err)
	}
	rawB, err := os.ReadFile(pathB)
	if err != nil {
		t.Fatal(err)
	}
	if string(rawA) != "tenant-a" {
		t.Fatalf("tenant A content = %q", string(rawA))
	}
	if string(rawB) != "tenant-b" {
		t.Fatalf("tenant B content = %q", string(rawB))
	}

	wantA := filepath.Join(base, "tenants", "slack_C001", "work", "marker.txt")
	wantB := filepath.Join(base, "tenants", "slack_C002", "work", "marker.txt")
	if pathA != wantA {
		t.Fatalf("tenant A path = %q, want %q", pathA, wantA)
	}
	if pathB != wantB {
		t.Fatalf("tenant B path = %q, want %q", pathB, wantB)
	}
}

func tenantCtx(sessionID string) context.Context {
	return context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID(sessionID))
}
