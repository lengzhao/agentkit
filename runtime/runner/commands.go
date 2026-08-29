package runner

import (
	"sort"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/loop"
)

func (r *Root) Commands() []agentkit.Command {
	agents := r.loopAgents()
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].ID() < agents[j].ID()
	})
	return []agentkit.Command{agent.Command(agents, r.sessionStore)}
}

func (r *Root) loopAgents() []agentkit.Agent {
	if ld, ok := r.loop.(*loop.Default); ok {
		return ld.Agents()
	}
	return nil
}
