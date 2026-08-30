package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	workspaceruntime "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestShellSlashCommand(t *testing.T) {
	root := t.TempDir()
	ws, err := workspaceruntime.New(workspaceruntime.Config{Global: root, Local: root, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := NewShellBash(ShellBashConfig{}, ShellBashDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	bundle, ok := tool.(*shellBashBundle)
	if !ok {
		t.Fatalf("expected *shellBashBundle, got %T", tool)
	}
	cmds := bundle.Commands()
	if len(cmds) != 1 {
		t.Fatalf("commands = %d, want 1", len(cmds))
	}
	if cmds[0].Name() != "shell" || cmds[0].Alias() != "sh" {
		t.Fatalf("name=%q alias=%q", cmds[0].Name(), cmds[0].Alias())
	}
	out, err := cmds[0].CommandExec(context.Background(), `echo hello`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Fatalf("output = %q", out)
	}
}

func TestShellSlashCommandUsage(t *testing.T) {
	cmd := shellSlashCommand{exec: &bashExecutor{workspace: stubWorkspace{root: t.TempDir()}}}
	if _, err := cmd.CommandExec(context.Background(), ""); err == nil {
		t.Fatal("expected usage error")
	}
}

func TestFormatShellOutput(t *testing.T) {
	out := formatShellOutput(ShellOutput{Stdout: "ok\n", Stderr: "warn", ExitCode: 2})
	if !strings.Contains(out, "ok") || !strings.Contains(out, "warn") || !strings.Contains(out, "[exit 2]") {
		t.Fatalf("unexpected output: %q", out)
	}
}

type stubWorkspace struct {
	root string
}

func (s stubWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	if rel == "" || rel == "." {
		return s.root, nil
	}
	return s.root + "/" + rel, nil
}

var (
	_ workspace.Service        = stubWorkspace{}
	_ agentkit.CommandProvider = (*shellBashBundle)(nil)
)
