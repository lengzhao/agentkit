package hookbeforestep

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	Services []compaction.Service `json:"services"`
}

type Provider struct {
	services []compaction.Service
}

func init() {
	pluginkit.Register("hook/before-step", New)
}

func New(_ Config, deps Deps) (agentkit.HookProvider, error) {
	return &Provider{services: deps.Services}, nil
}

func (p *Provider) Hooks() []agentkit.Hook {
	return []agentkit.Hook{
		agentkit.OnBeforeStep(10, p.beforeStep),
	}
}

func (p *Provider) beforeStep(ctx context.Context, step *agentkit.BeforeStep) error {
	messages := step.Messages
	for _, svc := range p.services {
		if svc == nil {
			continue
		}
		result, err := svc.Compact(ctx, compaction.Request{
			SessionID: step.SessionID,
			AgentID:   step.AgentID,
			Session:   step.Session,
			Messages:  messages,
		})
		if err != nil {
			return err
		}
		if len(result.Messages) > 0 {
			messages = result.Messages
		}
		if result.Applied && len(result.Messages) == 0 && step.Session != nil {
			derived, err := step.Session.DeriveMessages(ctx)
			if err != nil {
				return err
			}
			messages = derived
		}
	}
	step.Messages = messages
	return nil
}
