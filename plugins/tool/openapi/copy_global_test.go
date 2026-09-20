package openapi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/cap/workspace"
)

// fakeService resolves scoped paths against fixed global/local roots.
type fakeService struct {
	global string
	local  string
}

func (f fakeService) Resolve(_ context.Context, rel string) (string, error) {
	scope, path, scoped := workspace.ParseScoped(rel)
	if !scoped {
		return filepath.Join(f.local, filepath.FromSlash(rel)), nil
	}
	switch scope {
	case workspace.ScopeGlobal:
		return filepath.Join(f.global, filepath.FromSlash(path)), nil
	case workspace.ScopeLocal:
		return filepath.Join(f.local, filepath.FromSlash(path)), nil
	}
	return "", fmt.Errorf("unknown scope %q", scope)
}

func TestCopyLocalToGlobal(t *testing.T) {
	t.Parallel()

	svc := fakeService{global: t.TempDir(), local: t.TempDir()}
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

	got, err := copyLocalToGlobal(ctx, svc, "local:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != "global:api/pet.json" {
		t.Fatalf("copyLocalToGlobal = %q, want global:api/pet.json", got)
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

func TestCopyLocalToGlobalBarePath(t *testing.T) {
	t.Parallel()

	svc := fakeService{global: t.TempDir(), local: t.TempDir()}
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

	got, err := copyLocalToGlobal(ctx, svc, "api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != "global:api/pet.json" {
		t.Fatalf("copyLocalToGlobal = %q", got)
	}
}

func TestCopyLocalToGlobalAlreadyGlobal(t *testing.T) {
	t.Parallel()

	svc := fakeService{global: t.TempDir(), local: t.TempDir()}
	got, err := copyLocalToGlobal(context.Background(), svc, "global:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != "global:api/pet.json" {
		t.Fatalf("copyLocalToGlobal = %q, want unchanged", got)
	}
}
