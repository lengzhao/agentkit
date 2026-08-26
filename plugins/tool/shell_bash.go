package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type ShellBashConfig struct {
	// WorkDir is working directory relative to the workspace root.
	WorkDir string `json:"workDir"`
	// TimeoutSeconds is per-command limit; 0 falls back to the built-in default.
	TimeoutSeconds int `json:"timeoutSeconds"`
}

type ShellBashDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

type ShellInput struct {
	Command string `json:"command" jsonschema:"required,description=Shell command to execute"`
}

type ShellOutput struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

type bashExecutor struct {
	relWorkDir string
	timeout    time.Duration
	workspace  workspace.Service
}

// NewShellBash registers tool/shell-bash: Execute bash commands rooted in the workspace (tool name: bash).
//
// Best practices:
//   - Keep commands non-interactive; avoid pagers and prompts.
//   - For unattended runs pair with policy/shell-allowlist in strict mode.
func NewShellBash(cfg ShellBashConfig, deps ShellBashDeps) (agentkit.ToolPack, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/shell-bash requires workspace")
	}
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	exec := &bashExecutor{relWorkDir: workDir, timeout: timeout, workspace: deps.Workspace}

	tool, err := agentkit.NewTool[ShellInput, ShellOutput]("bash", func(ctx context.Context, input ShellInput) (ShellOutput, error) {
		return exec.run(ctx, input.Command)
	}).Description("Execute a bash command in the workspace.").Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(tool), nil
}

func (e *bashExecutor) run(ctx context.Context, command string) (ShellOutput, error) {
	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	workDir, err := e.workspace.Resolve(ctx, e.relWorkDir)
	if err != nil {
		return ShellOutput{}, err
	}

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
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
