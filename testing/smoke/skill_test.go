package smoke_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/skill"
	skilltool "github.com/lengzhao/agentkit/plugins/tool/skill"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type staticSkillRegistry struct {
	content skill.Content
}

func (r staticSkillRegistry) List(context.Context) ([]skill.Descriptor, error) {
	return []skill.Descriptor{{
		Name:        r.content.Name,
		Description: r.content.Description,
		Path:        r.content.Path,
	}}, nil
}

func (r staticSkillRegistry) Load(_ context.Context, name string) (skill.Content, error) {
	if name != r.content.Name {
		return skill.Content{}, fmt.Errorf("skill %q not found", name)
	}
	return r.content, nil
}

// E2E-510: loading a skill records skill/load in the session event log.
func TestSmokeSkillLoadEvent(t *testing.T) {
	t.Parallel()

	store, _ := agenttest.TempFileStore(t)
	reg := staticSkillRegistry{content: skill.Content{
		Name:        "demo",
		Description: "Demo skill for smoke tests.",
		Body:        "Follow these demo instructions.",
		Path:        "/tmp/demo",
	}}
	skillTool, err := skilltool.NewSkill(skilltool.SkillConfig{}, skilltool.SkillDeps{
		Skills:       reg,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}

	toolRT := agenttest.ToolsRuntime(t, tools.RuntimeDeps{
		Tools:    []agentkit.Tool{skillTool},
		Approval: agenttest.AllowAll{},
	})
	ag, _ := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{
			{
				ToolCalls: []agentkit.ToolCall{{
					ID: "call-skill", Name: "skill", Input: []byte(`{"name":"demo"}`),
				}},
			},
			{Text: "技能已加载。"},
		},
		Tools: toolRT,
		Store: store,
	})

	sessionID := agentkit.SessionID("smoke:skill")
	turnCtx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, turnCtx, ag, "加载 demo 技能")

	events := agenttest.SessionEvents(t, turnCtx, store, sessionID)
	agenttest.AssertEventAtLeast(t, events, agentkit.EventSkillLoad, 1)
	agenttest.AssertToolResultContains(t, events, "call-skill", "demo")
}
