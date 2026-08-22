package hookbeforestep

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	Services     []compaction.Service  `json:"services"`
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

type Provider struct {
	services     []compaction.Service
	sessionStore agentkit.SessionStore
}

func init() {
	pluginkit.Register("hook/before-step", New)
}

func New(_ Config, deps Deps) (agentkit.HookProvider, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("hook/before-step requires sessionStore dependency")
	}
	return &Provider{services: deps.Services, sessionStore: deps.SessionStore}, nil
}

func (p *Provider) Hooks() []agentkit.Hook {
	return []agentkit.Hook{
		agentkit.OnBeforeStep(10, p.beforeStep),
	}
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
