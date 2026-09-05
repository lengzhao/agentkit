package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/session"
)

func (r *Root) Commands() []agentkit.Command {
	agents := r.loopAgents()
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].ID() < agents[j].ID()
	})
	return []agentkit.Command{
		agent.Command(agents, r.sessionStore),
		agent.ACPCommand(agents, r.sessionStore),
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

func (r *Root) loopAgents() []agentkit.Agent {
	if ld, ok := r.loop.(*loop.Default); ok {
		return ld.Agents()
	}
	return nil
}
