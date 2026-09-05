package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/cap/workspace"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestResolveHome(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := rtworkspace.Resolve("~/.agentkit")
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
	scope, path, ok := rtworkspace.ParseScoped("global:skills")
	if !ok || scope != workspace.ScopeGlobal || path != "skills" {
		t.Fatalf("ParseScoped(global:skills)=%q %q %v", scope, path, ok)
	}
	scope, path, ok = rtworkspace.ParseScoped("local:")
	if !ok || scope != workspace.ScopeLocal || path != "." {
		t.Fatalf("ParseScoped(local:)=%q %q %v", scope, path, ok)
	}
	_, _, ok = rtworkspace.ParseScoped("/abs/path")
	if ok {
		t.Fatal("expected absolute path to be unscoped")
	}
}

func TestResolveRelParent(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	localRoot := filepath.Join(project, ".agentkit")
	if err := os.MkdirAll(localRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	examples := filepath.Join(project, "examples", "agents")
	if err := os.MkdirAll(examples, 0o755); err != nil {
		t.Fatal(err)
	}

	svc := rtworkspace.Static(localRoot)
	ctx := context.Background()

	got, err := svc.Resolve(ctx, "..")
	if err != nil {
		t.Fatal(err)
	}
	if got != project {
		t.Fatalf("Resolve(..)=%q want project root %q", got, project)
	}

	got, err = svc.Resolve(ctx, "../examples/agents")
	if err != nil {
		t.Fatal(err)
	}
	if got != examples {
		t.Fatalf("Resolve(../examples/agents)=%q want %q", got, examples)
	}

	if _, err := svc.Resolve(ctx, "../../etc/passwd"); err == nil {
		t.Fatal("expected path above project root to be rejected")
	}
}

func TestResolveRelRelative(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	got, err := rtworkspace.Static(base).Resolve(context.Background(), "sessions")
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
	got, err := rtworkspace.Static(t.TempDir()).Resolve(context.Background(), abs)
	if err != nil {
		t.Fatal(err)
	}
	if got != abs {
		t.Fatalf("Resolve(abs)=%q want %q", got, abs)
	}
}

func TestResolveRelStrictRefusesParent(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "tenant-a")
	for _, rel := range []string{"..", "../tenant-b", "../../etc", "sub/../../tenant-b"} {
		if got, err := rtworkspace.ResolveRelStrict(base, rel); err == nil {
			t.Fatalf("ResolveRelStrict(%q) = %q, want error", rel, got)
		}
	}
}

func TestResolveRelStrictAllowsOwnSubtree(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "tenant-a")
	got, err := rtworkspace.ResolveRelStrict(base, "sessions/log.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "sessions", "log.jsonl"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got, err := rtworkspace.ResolveRelStrict(base, "."); err != nil || got != base {
		t.Fatalf("dot = %q, %v", got, err)
	}
	if got, err := rtworkspace.ResolveRelStrict(base, "a/../b"); err != nil || got != filepath.Join(base, "b") {
		t.Fatalf("a/../b = %q, %v", got, err)
	}
}
