package hook

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	// ContributeCommands registers /compact on the shared commands registry.
	// Only one hook/before-step instance should enable this (typically the default agent).
	ContributeCommands bool `json:"contributeCommands"`
}

type Deps struct {
	Services     []compaction.Service  `json:"services"`
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

type Provider struct {
	contributeCommands bool
	services           []compaction.Service
	sessionStore       agentkit.SessionStore
}

func init() {
	pluginkit.Register("hook/before-step", New)
	pluginkit.Register("hook/turn-continue", NewTurnContinue)
}

// New registers hook/before-step: Run compaction services before each model step; contributes /compact.
//
// Best practices:
//   - Attach the same services here and to the agent's compaction dep: this hook covers the periodic check, the agent covers overflow recovery.
func New(cfg Config, deps Deps) (agentkit.HookProvider, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("hook/before-step requires sessionStore dependency")
	}
	return &Provider{
		contributeCommands: cfg.ContributeCommands,
		services:           deps.Services,
		sessionStore:       deps.SessionStore,
	}, nil
}

func (p *Provider) Hooks() []agentkit.Hook {
	return []agentkit.Hook{
		agentkit.OnBeforeStep(p.beforeStep),
	}
}

func (p *Provider) Commands() []agentkit.Command {
	if !p.contributeCommands || len(p.services) == 0 {
		return nil
	}
	return []agentkit.Command{compactCommand{
		sessionStore: p.sessionStore,
		services:     p.services,
	}}
}

type compactCommand struct {
	sessionStore agentkit.SessionStore
	services     []compaction.Service
}

func (compactCommand) Name() string        { return "compact" }
func (compactCommand) Alias() string       { return "" }
func (compactCommand) Description() string { return "run session compaction now" }

func (c compactCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if strings.TrimSpace(args) != "" {
		return "", fmt.Errorf("usage: /compact")
	}
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
	sess, err := c.sessionStore.Get(ctx, sessionID)
	if err != nil {
		return "", err
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		return "", err
	}
	_, applied, err := compaction.ApplyAll(ctx, c.services, compaction.Request{
		SessionID: sessionID,
		AgentID:   agentID,
		Session:   sess,
		Messages:  messages,
		Force:     true,
	})
	if err != nil {
		return "", err
	}
	if applied == 0 {
		return "compaction: nothing to compact", nil
	}
	return fmt.Sprintf("compaction: applied %d service(s)", applied), nil
}

func (p *Provider) beforeStep(ctx context.Context, step *agentkit.BeforeStep) error {
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
	var sess agentkit.Session
	if sessionID != "" {
		var err error
		sess, err = p.sessionStore.Get(ctx, sessionID)
		if err != nil {
			return err
		}
	}

	messages, _, err := compaction.ApplyAll(ctx, p.services, compaction.Request{
		SessionID: sessionID,
		AgentID:   agentID,
		Session:   sess,
		Messages:  step.Messages,
	})
	if err != nil {
		return err
	}
	step.Messages = messages
	return nil
}
