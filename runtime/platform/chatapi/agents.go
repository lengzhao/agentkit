package chatapi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func collectAgentIDs(configured []string, agents []agentkit.Agent) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(configured)+len(agents))
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range configured {
		add(id)
	}
	for _, ag := range agents {
		if ag == nil {
			continue
		}
		add(string(ag.ID()))
	}
	sort.Strings(out)
	return out
}

func (p *Platform) resolveInboundAgentID(override agentkit.AgentID, conv *conversation) agentkit.AgentID {
	overrideCfg := common.AgentRoutingConfig{AgentID: override}
	if id := overrideCfg.ResolveAgentID(); id != "" {
		return id
	}
	if conv != nil {
		if id := conv.agentID(); id != "" {
			return id
		}
	}
	return p.agentID
}

func (p *Platform) validateAgentID(id agentkit.AgentID) error {
	id = common.AgentRoutingConfig{AgentID: id}.ResolveAgentID()
	if id == "" || len(p.availableAgents) == 0 {
		return nil
	}
	for _, candidate := range p.availableAgents {
		if candidate == string(id) {
			return nil
		}
	}
	return fmt.Errorf("unknown agent %q", id)
}

func (c *conversation) bindAgent(id agentkit.AgentID) {
	id = common.AgentRoutingConfig{AgentID: id}.ResolveAgentID()
	if id != "" {
		c.AgentID = id
	}
}

func (c *conversation) agentID() agentkit.AgentID {
	if c == nil {
		return ""
	}
	return common.AgentRoutingConfig{AgentID: c.AgentID}.ResolveAgentID()
}
