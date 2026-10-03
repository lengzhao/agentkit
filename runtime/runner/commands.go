package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func (r *Root) Commands() []agentkit.Command {
	return []agentkit.Command{
		stopCommand{loop: r.loop, store: r.sessionStore},
		meCommand{},
	}
}

type meCommand struct{}

func (meCommand) Name() string        { return "me" }
func (meCommand) Alias() string       { return "whoami" }
func (meCommand) Description() string { return "show current user identity (uid/name/email)" }

func (meCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if strings.TrimSpace(args) != "" {
		return "", fmt.Errorf("usage: /me")
	}
	env := rctx.EnvelopeFromContext(ctx)
	uid := strings.TrimSpace(rctx.UserIDFromContext(ctx))
	name := strings.TrimSpace(env.Actor.Name)
	if name == "" {
		name = senderNameFromMetadata(env.Metadata)
	}
	email := strings.TrimSpace(env.Actor.Email)
	if email == "" {
		email = senderEmailFromMetadata(env.Metadata)
	}
	var b strings.Builder
	if uid != "" {
		fmt.Fprintf(&b, "uid: %s\n", uid)
	}
	if name != "" {
		fmt.Fprintf(&b, "name: %s\n", name)
	}
	if email != "" {
		fmt.Fprintf(&b, "email: %s\n", email)
	}
	out := strings.TrimRight(b.String(), "\n")
	if out == "" {
		return "unknown user", nil
	}
	return out, nil
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
	sessionID, busy, err := busyStopSession(ctx, c.store, c.loop)
	if err != nil {
		return "", err
	}
	if !busy || sessionID == "" {
		return "no turn in progress", nil
	}
	env := rctx.EnvelopeFromContext(ctx)
	env.Conversation = string(sessionID)
	cancelCtx := rctx.ApplyEnvelopeToContext(ctx, env)
	if err := c.loop.Cancel(cancelCtx, "/stop"); err != nil {
		return "", err
	}
	return "stopping current turn", nil
}
