package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type ShellBashConfig struct {
	// WorkDir is working directory relative to the workspace root.
	WorkDir string `json:"workDir"`
	// TimeoutSeconds is per-command limit; 0 falls back to the built-in default.
	TimeoutSeconds int `json:"timeoutSeconds"`
	// Commands optionally overrides env var names injected for shell-bash.<token>.
	// Default: scope is derived from the command's first token; keys come from L1 scopedEnv,
	// secrets.enc.json (/env add), or shell-bash.json manifest allowlist.
	Commands map[string][]string `json:"commands,omitempty"`
}

type ShellBashDeps struct {
	Workspace   workspace.Service `json:"workspace"`
	Credentials credentials.Store `json:"credentials,omitempty"`
}

type ShellInput struct {
	Command string `json:"command" jsonschema:"Shell command to execute"`
}

type ShellOutput struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

type bashExecutor struct {
	relWorkDir  string
	timeout     time.Duration
	workspace   workspace.Service
	credentials credentials.Store
	commands    map[string][]string
}

// NewShellBash registers tool/shell-bash: Execute bash commands rooted in the workspace (tool name: bash).
//
// Best practices:
//   - Keep commands non-interactive; avoid pagers and prompts.
//   - For unattended runs pair with policy/shell-allowlist in strict mode.
func NewShellBash(cfg ShellBashConfig, deps ShellBashDeps) (agentkit.Tool, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/shell-bash requires workspace")
	}
	if deps.Credentials != nil {
		if _, ok := deps.Credentials.(credentials.EnvPairResolver); !ok {
			slog.Warn("tool/shell-bash: deps.credentials is not credentials/integrations; scoped env injection disabled")
		}
	}
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	commands := cfg.Commands
	if commands == nil {
		commands = map[string][]string{}
	}
	exec := &bashExecutor{
		relWorkDir:  workDir,
		timeout:     timeout,
		workspace:   deps.Workspace,
		credentials: deps.Credentials,
		commands:    commands,
	}

	tool, err := agentkit.NewTool[ShellInput, ShellOutput]("bash", func(ctx context.Context, input ShellInput) (ShellOutput, error) {
		return exec.run(ctx, input.Command)
	}).Description("Execute a bash command in the workspace (default cwd is the absolute agent work directory). Prefer absolute paths in commands and when cd-ing to skill or global directories.").Build()
	if err != nil {
		return nil, err
	}
	return &shellBashBundle{tool: tool, exec: exec}, nil
}

func (e *bashExecutor) run(ctx context.Context, command string) (ShellOutput, error) {
	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	workDir, err := e.workspace.Resolve(ctx, e.relWorkDir)
	if err != nil {
		return ShellOutput{}, err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return ShellOutput{}, fmt.Errorf("mkdir work dir: %w", err)
	}

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = workDir
	cmd.Env = subprocessEnv(ctx, workDir, command, e.commands, e.credentials)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if runCtx.Err() != nil {
			return ShellOutput{}, fmt.Errorf("shell timeout after %s", e.timeout)
		} else {
			return ShellOutput{}, err
		}
	}
	return ShellOutput{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}
