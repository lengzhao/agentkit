package sessctx

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
)

// CwdCommand exposes the same session/channel binding scopes as /agent and /model.
func CwdCommand(store agentkit.SessionStore, ws workspace.Service) agentkit.Command {
	return cwdCommand{store: store, workspace: ws}
}

type cwdCommand struct {
	store     agentkit.SessionStore
	workspace workspace.Service
}

func (cwdCommand) Name() string  { return "cwd" }
func (cwdCommand) Alias() string { return "" }
func (cwdCommand) Description() string {
	return "show or bind session or channel (-c) working directory"
}
func (c cwdCommand) CommandExec(ctx context.Context, args string) (string, error) {
	global, rest := agentkit.PeelGlobalFlag(strings.Fields(args))
	channel := false
	fields := rest
	rest = nil
	for _, field := range fields {
		if field == "-c" || field == "--channel" {
			channel = true
		} else {
			rest = append(rest, field)
		}
	}
	if global {
		return "", fmt.Errorf("/cwd supports session or -c channel scope; no global binding")
	}
	entry := rctx.ActiveEntryKeyFromContext(ctx)
	if entry == "" {
		entry = rctx.SessionIDFromContext(ctx)
	}
	sid, err := capsession.ResolveActiveSessionID(ctx, c.store, entry)
	if err != nil {
		return "", err
	}
	stack := sessbind.StackFromContext(ctx, c.workspace).WithSession(sid)
	scope := sessbind.LevelSession
	if channel {
		scope = sessbind.LevelChannel
	}
	kind := sessbind.CwdBinding{}
	if len(rest) == 0 {
		effective, winner, hits, err := stack.EffectiveWithLevel(ctx, c.store, kind)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		if effective == "" {
			b.WriteString("Cwd: agent/workspace default (no binding)\n")
		} else {
			fmt.Fprintf(&b, "Cwd: %s (%s)\n", effective, winner)
		}
		for _, level := range sessbind.HitsFor(hits) {
			fmt.Fprintf(&b, "%s override: %s\n", level, hits[level])
		}
		b.WriteString("/cwd use <path> | /cwd -c use <path> | /cwd [-c] reset\nChanges apply on the next turn; A directory change starts a fresh model context; the audit log is retained.")
		return b.String(), nil
	}
	value := ""
	switch {
	case len(rest) == 1 && (rest[0] == "reset" || rest[0] == "default"):
	case len(rest) >= 2 && rest[0] == "use":
		value, err = sessbind.ValidateCwd(ctx, c.workspace, strings.Join(rest[1:], " "))
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("usage: /cwd [-c] use <path> | /cwd [-c] reset")
	}
	if err := stack.Set(ctx, c.store, kind, scope, value); err != nil {
		return "", err
	}
	if value == "" {
		return fmt.Sprintf("%s cwd reset; applies on next turn", scope), nil
	}
	return fmt.Sprintf("%s cwd: %s; applies on next turn", scope, value), nil
}
