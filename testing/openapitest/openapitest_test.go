package openapitest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/agentkit/testing/openapitest"
)

func TestMockGetPetWithAuth(t *testing.T) {
	mock := openapitest.StartMock(t)
	root := openapitest.Materialize(t, mock.URL)
	provider := openapitest.NewProvider(t, root)
	ctx := openapitest.TurnContext(agentkit.SessionID("test:openapi-get"), agentkit.AgentID("smoke"), "user-42", nil)

	tool := openapitest.ToolByName(t, ctx, provider, "petstore__getPet")
	out := agenttest.CallTool(t, ctx, tool, `{"id":"42","verbose":true}`)
	if !strings.Contains(out, `"name":"Rex"`) {
		t.Fatalf("output = %s", out)
	}
}

func TestMockBindUserHeader(t *testing.T) {
	mock := openapitest.StartMock(t)
	root := openapitest.Materialize(t, mock.URL)
	provider := openapitest.NewProvider(t, root)
	ctx := openapitest.TurnContext(agentkit.SessionID("test:openapi"), agentkit.AgentID("smoke"), "user-42", nil)

	tool := openapitest.ToolByName(t, ctx, provider, "petstore__listOrders")
	schemaRaw, err := json.Marshal(tool.InputSchema())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(schemaRaw), `"uid"`) {
		t.Fatalf("bound uid should be hidden: %s", schemaRaw)
	}

	out := agenttest.CallTool(t, ctx, tool, `{"page":2}`)
	var payload struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, out)
	}
	if payload.Status != http.StatusOK {
		t.Fatalf("status = %d body=%s", payload.Status, out)
	}
}

func TestMockPathFixture(t *testing.T) {
	mock := openapitest.StartMock(t)
	root := openapitest.Materialize(t, mock.URL)
	provider := openapitest.NewProvider(t, root)
	ctx := context.Background()

	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("tools = %d, want getPet+createPet+listOrders+ping (deletePet denied)", len(tools))
	}
}
