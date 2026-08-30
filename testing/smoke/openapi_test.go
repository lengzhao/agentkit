package smoke_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/agentkit/testing/openapitest"
)

func TestSmokeOpenAPIDynamicTools(t *testing.T) {
	mock := openapitest.StartMock(t)
	root := openapitest.Materialize(t, mock.URL)
	provider := openapitest.NewProvider(t, root)
	ctx := openapitest.TurnContext(agentkit.SessionID("smoke:openapi"), agentkit.AgentID("smoke"), "user-42", nil)

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

	events := agenttest.SessionEvents(t, ctx, store, agentkit.SessionID("smoke:openapi"))
	agenttest.AssertToolResultContains(t, events, "call-get", `"name":"Rex"`)
	agenttest.AssertToolResultContains(t, events, "call-create", `"name":"Fido"`)
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
}

func TestSmokeOpenAPIProviderDirect(t *testing.T) {
	mock := openapitest.StartMock(t)
	root := openapitest.Materialize(t, mock.URL)
	provider := openapitest.NewProvider(t, root)
	ctx := context.Background()

	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("tools = %d, want getPet/createPet/listOrders/ping", len(tools))
	}

	ping := openapitest.ToolByName(t, ctx, provider, "petstore__ping")
	out := agenttest.CallTool(t, ctx, ping, `{}`)
	if out == "" {
		t.Fatal("expected ping result")
	}
}
