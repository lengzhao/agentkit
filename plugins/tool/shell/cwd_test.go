package shell_test

import (
	"context"
	capshell "github.com/lengzhao/agentkit/cap/shell"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/tool/shell"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellUsesTurnCwdAndHonorsExplicitRequestDirectory(t *testing.T) {
	root := t.TempDir()
	project, _ := filepath.EvalSymlinks(t.TempDir())
	ex := newExecutor(t, shell.ShellBashConfig{WorkDir: "."}, root)
	ctx := workspace.WithWorkingDirectory(context.Background(), project)
	out, err := ex.Run(ctx, capshell.Request{Command: "pwd"})
	if err != nil || strings.TrimSpace(out.Stdout) != project {
		t.Fatalf("default cwd: %q %v", out.Stdout, err)
	}
	explicit, _ := filepath.EvalSymlinks(t.TempDir())
	out, err = ex.Run(ctx, capshell.Request{Command: "pwd", WorkDir: explicit})
	if err != nil || strings.TrimSpace(out.Stdout) != explicit {
		t.Fatalf("explicit cwd: %q %v", out.Stdout, err)
	}
}
