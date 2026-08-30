//go:build integration

package integration_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/agentkit/testing/openapitest"
)

// E2E-502: OpenAPI dynamic tools against a real HTTP mock server through a full agent turn.
func TestIntegrationOpenAPIAgentTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("integration openapi")
	}

	mock := openapitest.StartMock(t)
	root := openapitest.Materialize(t, mock.URL)
	provider := openapitest.NewProvider(t, root)
	ctx := openapitest.TurnContext(agentkit.SessionID("it:openapi"), agentkit.AgentID("smoke"), "user-42", nil)

	toolRT := agenttest.ToolsRuntime(t, tools.RuntimeDeps{
		DynamicTools: []agentkit.ToolProvider{provider},
		Approval:     agenttest.AllowAll{},
	})
	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{
			{
				ToolCalls: []agentkit.ToolCall{{
					ID: "call-get", Name: "petstore__getPet", Input: []byte(`{"id":"42","verbose":true}`),
				}},
			},
			{
				ToolCalls: []agentkit.ToolCall{{
					ID: "call-create", Name: "petstore__createPet", Input: []byte(`{"body":{"name":"Fido"}}`),
				}},
			},
			{Text: "已通过 OpenAPI 工具读取并创建宠物。"},
		},
		Tools: toolRT,
	})

	agenttest.RunTurn(t, ctx, ag, "用 OpenAPI 工具查宠物并创建一只")

	events := agenttest.SessionEvents(t, ctx, store, agentkit.SessionID("it:openapi"))
	agenttest.AssertToolResultContains(t, events, "call-get", `"name":"Rex"`)
	agenttest.AssertToolResultContains(t, events, "call-create", `"name":"Fido"`)
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
}
