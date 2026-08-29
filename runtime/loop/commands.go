package loop

import (
	"sort"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
)

func (l *Default) Commands() []agentkit.Command {
	agents := make([]agentkit.Agent, 0, len(l.agents))
	for _, ag := range l.agents {
		agents = append(agents, ag)
	}
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].ID() < agents[j].ID()
	})
	return []agentkit.Command{agent.Command(agents, l.sessionStore)}
}
