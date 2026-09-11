package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
	rw "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestCopyLocalToGlobal(t *testing.T) {
	t.Parallel()

	globalRoot := t.TempDir()
	localRoot := t.TempDir()
	svc, err := rw.New(rw.Config{
		Global: globalRoot,
		Local:  localRoot,
		Scope:  cw.ScopeLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	localAbs, err := svc.Resolve(ctx, "local:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(localAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localAbs, []byte("spec"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := rw.CopyLocalToGlobal(ctx, svc, "local:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != "global:api/pet.json" {
		t.Fatalf("CopyLocalToGlobal = %q, want global:api/pet.json", got)
	}

	globalAbs, err := svc.Resolve(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(globalAbs)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "spec" {
		t.Fatalf("global file = %q", body)
	}
}

func TestCopyLocalToGlobalTenant(t *testing.T) {
	t.Parallel()

	globalRoot := t.TempDir()
	localBase := filepath.Join(globalRoot, "tenants")
	svc, err := rw.NewTenant(rw.TenantConfig{
		Global:    globalRoot,
		LocalBase: localBase,
		Scope:     cw.ScopeLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: "slack:C001",
		Workspace:    "slack:C001",
	})

	localAbs, err := svc.Resolve(ctx, "local:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(localAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localAbs, []byte("tenant-spec"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := rw.CopyLocalToGlobal(ctx, svc, "api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != "global:api/pet.json" {
		t.Fatalf("CopyLocalToGlobal = %q", got)
	}

	globalAbs := filepath.Join(globalRoot, "api", "pet.json")
	body, err := os.ReadFile(globalAbs)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "tenant-spec" {
		t.Fatalf("global file = %q", body)
	}
}
