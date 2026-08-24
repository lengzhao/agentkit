package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHome(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Resolve("~/.agentkit")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".agentkit")
	if got != want {
		t.Fatalf("Resolve(~/.agentkit)=%q want %q", got, want)
	}
}

func TestParseScoped(t *testing.T) {
	t.Parallel()
	scope, path, ok := ParseScoped("global:skills")
	if !ok || scope != ScopeGlobal || path != "skills" {
		t.Fatalf("ParseScoped(global:skills)=%q %q %v", scope, path, ok)
	}
	scope, path, ok = ParseScoped("local:")
	if !ok || scope != ScopeLocal || path != "." {
		t.Fatalf("ParseScoped(local:)=%q %q %v", scope, path, ok)
	}
	_, _, ok = ParseScoped("/abs/path")
	if ok {
		t.Fatal("expected absolute path to be unscoped")
	}
}

func TestResolveRelRelative(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	got, err := Static(base).Resolve(context.Background(), "sessions")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "sessions")
	if got != want {
		t.Fatalf("Resolve(sessions)=%q want %q", got, want)
	}
}

func TestResolveRelAbsolute(t *testing.T) {
	t.Parallel()
	abs := t.TempDir()
	got, err := Static(t.TempDir()).Resolve(context.Background(), abs)
	if err != nil {
		t.Fatal(err)
	}
	if got != abs {
		t.Fatalf("Resolve(abs)=%q want %q", got, abs)
	}
}
