package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	rw "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestDefaultResolveScopedAndDefaultScope(t *testing.T) {
	t.Parallel()

	global := t.TempDir()
	local := t.TempDir()

	svc, err := rw.New(rw.Config{
		Global: global,
		Local:  local,
		Scope:  "global",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	got, err := svc.Resolve(ctx, "sessions")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(global, "sessions"); got != want {
		t.Fatalf("default scope resolve=%q want %q", got, want)
	}

	got, err = svc.Resolve(ctx, "local:sessions")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(local, "sessions"); got != want {
		t.Fatalf("local: resolve=%q want %q", got, want)
	}

	got, err = svc.Resolve(ctx, "global:.")
	if err != nil {
		t.Fatal(err)
	}
	if got != global {
		t.Fatalf("global:. resolve=%q want %q", got, global)
	}
}

func TestDefaultLocalScope(t *testing.T) {
	t.Parallel()

	global := t.TempDir()
	local := t.TempDir()

	svc, err := rw.New(rw.Config{
		Global: global,
		Local:  local,
		Scope:  "local",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Resolve(context.Background(), "src")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(local, "src"); got != want {
		t.Fatalf("local scope resolve=%q want %q", got, want)
	}
}

func TestDefaultEmptyLocalUsesAgentkit(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	svc, err := rw.New(rw.Config{Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Resolve(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, ".agentkit")
	if got != want {
		t.Fatalf("default local root=%q want %q", got, want)
	}
}

func TestDefaultRootAlias(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".agentkit")

	svc, err := rw.New(rw.Config{Root: "~/.agentkit"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Resolve(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("root alias resolve=%q want %q", got, want)
	}
}
