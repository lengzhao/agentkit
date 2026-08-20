package loop

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
)

type Config struct {
	DefaultAgent agentkit.AgentID `json:"defaultAgent"`
}

type Deps struct {
	Agents []agentkit.Agent `json:"agents"`
}

type Default struct {
	agents       map[agentkit.AgentID]agentkit.Agent
	defaultAgent agentkit.AgentID
}

func New(cfg Config, deps Deps) (*Default, error) {
	agents := make(map[agentkit.AgentID]agentkit.Agent, len(deps.Agents))
	for _, ag := range deps.Agents {
		if ag == nil {
			continue
		}
		agents[ag.ID()] = ag
	}
	defaultID := cfg.DefaultAgent
	if defaultID == "" && len(deps.Agents) > 0 {
		defaultID = deps.Agents[0].ID()
	}
	return &Default{agents: agents, defaultAgent: defaultID}, nil
}

func (l *Default) Dispatch(ctx context.Context, req agentkit.LoopRequest) (agentkit.LoopResult, error) {
	agentID := req.Event.AgentID
	if agentID == "" {
		agentID = l.defaultAgent
	}
	ag, ok := l.agents[agentID]
	if !ok {
		return agentkit.LoopResult{}, errAgentNotFound(agentID)
	}
	result, err := ag.RunTurn(ctx, agentkit.TurnInput{Message: req.Event.Message})
	if err != nil {
		return agentkit.LoopResult{}, err
	}
	var outbound []agentkit.OutboundEvent
	for _, msg := range result.Messages {
		if msg.Role != "assistant" {
			continue
		}
		raw, _ := json.Marshal(msg)
		outbound = append(outbound, agentkit.OutboundEvent{
			SessionID: ag.Session().ID(),
			AgentID:   ag.ID(),
			Type:      agentkit.EventAssistantMessage,
			Data:      raw,
		})
	}
	return agentkit.LoopResult{Outbound: outbound}, nil
}

type agentNotFoundError struct{ id agentkit.AgentID }

func (e agentNotFoundError) Error() string { return "agent not found: " + string(e.id) }
func errAgentNotFound(id agentkit.AgentID) error { return agentNotFoundError{id} }
