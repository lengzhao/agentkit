package smoke_test

import (
	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/plugins/tool/finish"
	subagentplugin "github.com/lengzhao/agentkit/plugins/tool/subagent"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// subagentDelegateConfig returns the base delegate wiring with the real plugin
// constructors injected; agenttest itself stays plugin-agnostic.
func subagentDelegateConfig() agenttest.SubagentDelegateConfig {
	return agenttest.SubagentDelegateConfig{
		NewFinishTool: func(store agentkit.SessionStore) (agentkit.Tool, error) {
			events, err := sessevents.New()
			if err != nil {
				return nil, err
			}
			return finish.NewFinish(finish.FinishConfig{}, finish.FinishDeps{SessionStore: store, SessionEvents: events})
		},
		NewDelegateTool: func(spawner capsubagent.Spawner) (agentkit.Tool, error) {
			return subagentplugin.NewSubagent(subagentplugin.SubagentConfig{}, subagentplugin.SubagentDeps{Subagent: spawner})
		},
	}
}
