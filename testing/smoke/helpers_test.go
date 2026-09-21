package smoke_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/plugins/tool/finish"
	subagentplugin "github.com/lengzhao/agentkit/plugins/tool/subagent"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func mustTelemetry(t *testing.T) captelemetry.Toolkit {
	t.Helper()
	tk, err := rttelemetry.NewToolkit(struct{}{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

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
