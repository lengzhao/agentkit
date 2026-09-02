package bootstrap_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/bootstrap"
)

func TestShellInitApp(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	project := filepath.Join(root, "project")
	local := filepath.Join(project, ".agentkit")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}

	ws := dualWorkspace(t, filepath.Join(root, "global"), local)
	init, err := bootstrap.NewShell(bootstrap.ShellConfig{
		WorkDir: "local:..",
		Commands: []string{
			"mkdir -p seeded && echo hello > seeded/note.txt",
		},
	}, bootstrap.ShellDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	if err := init.InitApp(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(project, "seeded", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("content = %q", string(got))
	}
}

func TestShellGitInit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	t.Parallel()

	root := t.TempDir()
	project := filepath.Join(root, "project")
	local := filepath.Join(project, ".agentkit")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}

	ws := dualWorkspace(t, filepath.Join(root, "global"), local)
	init, err := bootstrap.NewShell(bootstrap.ShellConfig{
		WorkDir: "local:..",
		Commands: []string{
			"test -d .git || git init",
		},
	}, bootstrap.ShellDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	if err := init.InitApp(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".git")); err != nil {
		t.Fatalf(".git missing: %v", err)
	}
	if err := init.InitApp(context.Background()); err != nil {
		t.Fatalf("second init: %v", err)
	}
}

func dualWorkspace(t *testing.T, global, local string) workspace.Service {
	t.Helper()
	svc, err := newScopedWorkspace(global, local)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

type scopedWorkspace struct {
	global string
	local  string
}

func newScopedWorkspace(global, local string) (workspace.Service, error) {
	g, err := workspace.Resolve(global)
	if err != nil {
		return nil, err
	}
	l, err := workspace.Resolve(local)
	if err != nil {
		return nil, err
	}
	return &scopedWorkspace{global: g, local: l}, nil
}

func (s *scopedWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	if scope, path, ok := workspace.ParseScoped(rel); ok {
		switch scope {
		case workspace.ScopeGlobal:
			return workspace.ResolveRel(s.global, path)
		case workspace.ScopeLocal:
			return workspace.ResolveRel(s.local, path)
		}
	}
	return workspace.ResolveRel(s.local, rel)
}
