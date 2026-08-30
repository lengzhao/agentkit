package send

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

type sendBundle struct {
	tool agentkit.Tool
	cfg  SendConfig
	deps SendDeps
}

func (b *sendBundle) Name() string { return b.tool.Name() }

func (b *sendBundle) Description() string { return b.tool.Description() }

func (b *sendBundle) InputSchema() agentkit.JSONSchema { return b.tool.InputSchema() }

func (b *sendBundle) Call(ctx context.Context, input json.RawMessage) (string, error) {
	return b.tool.Call(ctx, input)
}

func (b *sendBundle) Commands() []agentkit.Command {
	return []agentkit.Command{sendSlashCommand{bundle: b}}
}

type sendSlashCommand struct {
	bundle *sendBundle
}

func (sendSlashCommand) Name() string { return "send" }

func (sendSlashCommand) Alias() string { return "" }

func (sendSlashCommand) Description() string {
	return "send a proactive message to the current inbox, a target session, or a user without invoking the model"
}

func (c sendSlashCommand) CommandExec(ctx context.Context, args string) (string, error) {
	input, err := ParseSlashArgs(args)
	if err != nil {
		return "", err
	}
	if err := Dispatch(ctx, c.bundle.deps, c.bundle.cfg, input); err != nil {
		return "", err
	}
	target := formatSendTarget(input)
	return fmt.Sprintf("sent to %s", target), nil
}

func formatSendTarget(input SendInput) string {
	if id := strings.TrimSpace(input.SessionID); id != "" {
		return id
	}
	if id := strings.TrimSpace(input.UserID); id != "" {
		return "@" + id
	}
	return "current inbox"
}
