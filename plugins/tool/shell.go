package tool

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/shell"
)

type ShellConfig struct{}

type ShellDeps struct {
	Shell shell.Executor `json:"shell"`
}

type ShellInput struct {
	Command string `json:"command" jsonschema:"required,description=Shell command to execute"`
}

type ShellOutput struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// NewShell registers tool/shell: Execute a shell command through the configured executor.
//
// Best practices:
//   - Keep commands non-interactive; avoid pagers and prompts.
//   - For unattended runs pair with policy/shell-allowlist in strict mode.
//   - The per-command timeout comes from the shell dep, not from this tool.
func NewShell(_ ShellConfig, deps ShellDeps) (agentkit.Tool, error) {
	if deps.Shell == nil {
		return nil, fmt.Errorf("tool/shell requires shell dependency")
	}
	return agentkit.NewTool("bash", func(ctx context.Context, input ShellInput) (ShellOutput, error) {
		result, err := deps.Shell.Run(ctx, shell.Request{Command: input.Command})
		if err != nil {
			return ShellOutput{}, err
		}
		return ShellOutput{
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, nil
	}).Description("Execute a bash command in the workspace.").Build()
}
