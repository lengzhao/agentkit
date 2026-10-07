package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/tooloutput"
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

func (shellSlashCommand) SanitizeArgsForLog(args string) string {
	return agentkit.RedactSlashArgsForLog(args)
}

func (c shellSlashCommand) CommandExec(ctx context.Context, args string) (string, error) {
	command := strings.TrimSpace(args)
	if command == "" {
		return "", fmt.Errorf("usage: /shell <command>")
	}
	out, err := c.exec.run(ctx, command, nil)
	if err != nil {
		if te, ok := tooloutput.AsTimeoutError(err); ok {
			return "", fmt.Errorf("shell timeout after %g seconds", te.Seconds)
		}
		return "", err
	}
	return tooloutput.SlashCommandText(out.Stdout, out.Stderr, out.ExitCode), nil
}
