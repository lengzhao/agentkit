package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func (r *Root) Commands() []agentkit.Command {
	return []agentkit.Command{
		stopCommand{loop: r.loop, store: r.sessionStore},
	}
}

type stopCommand struct {
	loop  agentkit.Loop
	store agentkit.SessionStore
}

func (stopCommand) Name() string        { return "stop" }
func (stopCommand) Alias() string       { return "" }
func (stopCommand) Description() string { return "stop the current turn for this session" }

func (c stopCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if strings.TrimSpace(args) != "" {
		return "", fmt.Errorf("usage: /stop")
	}
	entryKey := session.SessionIDFromContext(ctx)
	sessionID, err := session.ResolveActiveSessionID(ctx, c.store, entryKey)
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	if !c.loop.IsSessionBusy(sessionID) {
		return "no turn in progress", nil
	}
	env := session.EnvelopeFromContext(ctx)
	env.Conversation = string(sessionID)
	cancelCtx := session.ApplyEnvelopeToContext(ctx, env)
	if err := c.loop.Cancel(cancelCtx, "/stop"); err != nil {
		return "", err
	}
	return "stopping current turn", nil
}
