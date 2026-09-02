package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/pluginkit"
)

type ShellConfig struct {
	// WorkDir is the workspace-relative working directory. Default local:..
	WorkDir string `json:"workDir"`
	// Commands run sequentially via bash -lc at app start.
	Commands []string `json:"commands"`
	// TimeoutSeconds bounds each command; 0 uses 60s.
	TimeoutSeconds int `json:"timeoutSeconds"`
}

type ShellDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

type shellInit struct {
	workDir   string
	commands  []string
	timeout   time.Duration
	workspace workspace.Service
}

func init() {
	pluginkit.Register("bootstrap/shell", NewShell)
}

// NewShell registers bootstrap/shell: Run shell commands in the workspace at app start.
//
// Best practices:
//   - Keep commands idempotent (test -d, cp -n, git init only when missing).
//   - Use local:. for startup init (works with workspace/tenant; no tenant context yet).
//   - Use local:.. when local root is .agentkit and commands target the project tree (workspace/default only).
func NewShell(cfg ShellConfig, deps ShellDeps) (agentkit.AppInitializer, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("bootstrap/shell requires workspace")
	}
	if len(cfg.Commands) == 0 {
		return nil, fmt.Errorf("bootstrap/shell requires at least one command")
	}
	workDir := strings.TrimSpace(cfg.WorkDir)
	if workDir == "" {
		workDir = "local:."
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &shellInit{
		workDir:   workDir,
		commands:  cfg.Commands,
		timeout:   timeout,
		workspace: deps.Workspace,
	}, nil
}

func (s *shellInit) InitApp(ctx context.Context) error {
	workDir, err := s.workspace.Resolve(ctx, s.workDir)
	if err != nil {
		return fmt.Errorf("resolve workDir %q: %w", s.workDir, err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("mkdir work dir: %w", err)
	}
	for i, command := range s.commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		runCtx, cancel := context.WithTimeout(ctx, s.timeout)
		cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			return fmt.Errorf("command[%d] %q: %w: %s", i, command, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

var _ agentkit.AppInitializer = (*shellInit)(nil)
