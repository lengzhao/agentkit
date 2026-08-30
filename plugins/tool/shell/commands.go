package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

type shellBashBundle struct {
	tool agentkit.Tool
	exec *bashExecutor
}

func (b *shellBashBundle) Name() string { return b.tool.Name() }

func (b *shellBashBundle) Description() string { return b.tool.Description() }

func (b *shellBashBundle) InputSchema() agentkit.JSONSchema { return b.tool.InputSchema() }

func (b *shellBashBundle) Call(ctx context.Context, input json.RawMessage) (string, error) {
	return b.tool.Call(ctx, input)
}

func (b *shellBashBundle) Commands() []agentkit.Command {
	return []agentkit.Command{shellSlashCommand{exec: b.exec}}
}

type shellSlashCommand struct {
	exec *bashExecutor
}

func (shellSlashCommand) Name() string  { return "shell" }
func (shellSlashCommand) Alias() string { return "sh" }
func (shellSlashCommand) Description() string {
	return "run a shell command locally without invoking the model"
}

func (c shellSlashCommand) CommandExec(ctx context.Context, args string) (string, error) {
	command := strings.TrimSpace(args)
	if command == "" {
		return "", fmt.Errorf("usage: /shell <command>")
	}
	out, err := c.exec.run(ctx, command)
	if err != nil {
		return "", err
	}
	return formatShellOutput(out), nil
}

func formatShellOutput(out ShellOutput) string {
	var b strings.Builder
	if out.Stdout != "" {
		b.WriteString(out.Stdout)
	}
	if out.Stderr != "" {
		if b.Len() > 0 && !strings.HasSuffix(out.Stdout, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(out.Stderr)
	}
	if out.ExitCode != 0 {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[exit %d]", out.ExitCode)
	}
	return strings.TrimRight(b.String(), "\n")
}
