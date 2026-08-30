package common

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// SlashOutcomeKind describes how an inbound slash command was resolved.
type SlashOutcomeKind int

const (
	// SlashNotCommand means the text is not a slash command.
	SlashNotCommand SlashOutcomeKind = iota
	// SlashHandled means the platform should reply locally and not enqueue a turn.
	SlashHandled
	// SlashForward means the command is unknown; notify the user then forward to the agent.
	SlashForward
)

// SlashOutcome is the result of ProcessSlash.
type SlashOutcome struct {
	Kind  SlashOutcomeKind
	Reply string
}

// SlashContext carries delivery routing and sessionScope for slash commands.
type SlashContext struct {
	DeliverySessionID agentkit.SessionID
	PlatformID        string
	SessionScope      session.SessionScope
	UserID            string
}

// IsSlashCommand reports whether text starts with a slash command.
func IsSlashCommand(text string) bool {
	_, _, ok := ParseSlashCommand(text)
	return ok
}

// ParseSlashCommand splits "/name args" into name and args.
func ParseSlashCommand(line string) (name, args string, ok bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return "", "", false
	}
	body := strings.TrimSpace(strings.TrimPrefix(line, "/"))
	if body == "" {
		return "", "", true
	}
	fields := strings.Fields(body)
	name = strings.ToLower(fields[0])
	if len(fields) > 1 {
		args = strings.TrimSpace(body[len(fields[0]):])
	}
	return name, args, true
}

// ProcessSlash resolves slash commands via the injected commands registry.
// Non-slash input returns SlashNotCommand.
func ProcessSlash(ctx context.Context, commands agentkit.Commands, slash SlashContext, text string) (SlashOutcome, error) {
	name, args, ok := ParseSlashCommand(text)
	if !ok {
		return SlashOutcome{Kind: SlashNotCommand}, nil
	}
	if name == "" {
		return SlashOutcome{Kind: SlashHandled, Reply: FormatHelp(commands)}, nil
	}
	switch name {
	case "help", "h", "?":
		topic := strings.TrimSpace(args)
		if topic == "" {
			return SlashOutcome{Kind: SlashHandled, Reply: FormatHelp(commands)}, nil
		}
		return SlashOutcome{
			Kind:  SlashHandled,
			Reply: "用法: /help\n      /help <topic>（平台暂仅支持列出命令）",
		}, nil
	}
	if commands == nil {
		return SlashOutcome{
			Kind:  SlashForward,
			Reply: FormatUnknownCommand(name),
		}, nil
	}

	entryKey := session.ActiveSessionEntryKey(slash.PlatformID, slash.DeliverySessionID, slash.SessionScope, slash.UserID)
	if entryKey == "" {
		entryKey = slash.DeliverySessionID
	}
	cmdCtx := context.WithValue(ctx, agentkit.KeySessionID, entryKey)
	if slash.DeliverySessionID != "" {
		cmdCtx = context.WithValue(cmdCtx, agentkit.KeyDeliverySessionID, slash.DeliverySessionID)
	}
	if slash.UserID != "" {
		cmdCtx = context.WithValue(cmdCtx, agentkit.KeyUserID, slash.UserID)
	}
	out, err := commands.Dispatch(cmdCtx, name, args)
	if errors.Is(err, agentkit.ErrCommandNotHandled) {
		return SlashOutcome{
			Kind:  SlashForward,
			Reply: FormatUnknownCommand(name),
		}, nil
	}
	if err != nil {
		return SlashOutcome{}, err
	}
	return SlashOutcome{Kind: SlashHandled, Reply: out}, nil
}

// FormatHelp renders a plain-text command list for IM platforms.
func FormatHelp(commands agentkit.Commands) string {
	var b strings.Builder
	b.WriteString("可用命令:\n")
	b.WriteString("  /help, /h, /?   显示帮助\n")
	if commands != nil {
		for _, cmd := range commands.List() {
			line := fmt.Sprintf("  /%-14s %s", cmd.Name(), cmd.Description())
			if alias := cmd.Alias(); alias != "" {
				line += fmt.Sprintf(" (别名: %s)", alias)
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// FormatUnknownCommand matches cc-connect's unknown slash forwarding notice.
func FormatUnknownCommand(name string) string {
	return fmt.Sprintf("`/%s` 不是已注册命令，转发给 Agent...", name)
}

