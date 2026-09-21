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
	fs := testFSOver(t, svc)
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

	wantGlobal, err := svc.Resolve(ctx, "global:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := copyLocalToGlobal(ctx, fs, svc, "local:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != wantGlobal {
		t.Fatalf("copyLocalToGlobal = %q, want %q", got, wantGlobal)
	}

	globalAbs := got
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
	fs := testFSOver(t, svc)
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

	wantGlobal, err := svc.Resolve(ctx, "global:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := copyLocalToGlobal(ctx, fs, svc, "api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != wantGlobal {
		t.Fatalf("copyLocalToGlobal = %q, want %q", got, wantGlobal)
	}
}

func TestCopyLocalToGlobalAlreadyGlobal(t *testing.T) {
	t.Parallel()

	svc := fakeService{global: t.TempDir(), local: t.TempDir()}
	fs := testFSOver(t, svc)
	want, err := svc.Resolve(context.Background(), "global:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := copyLocalToGlobal(context.Background(), fs, svc, "global:api/pet.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("copyLocalToGlobal = %q, want %q", got, want)
	}
}
