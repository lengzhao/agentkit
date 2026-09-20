package openapitest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/workspace"
	openapiplugin "github.com/lengzhao/agentkit/plugins/tool/openapi"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/agentkit/testing/openapitest"
)

// newOpenAPIProvider injects the real tool/openapi constructor into openapitest,
// keeping the helper package plugin-agnostic.
func newOpenAPIProvider(ws workspace.Service, creds credentials.Store) (agentkit.ToolProvider, error) {
	return openapiplugin.NewOpenAPI(openapiplugin.OpenAPIConfig{
		EnableLocal: true,
		Files:       []string{"api.json", "local:api.json"},
	}, openapiplugin.OpenAPIDeps{
		Workspace:   ws,
		Credentials: creds,
		ConfigFile:  newTestConfigFileWriter(),
	})
}

func TestMockGetPetWithAuth(t *testing.T) {
	mock := openapitest.StartMock(t)
	root := openapitest.Materialize(t, mock.URL)
	provider := openapitest.NewProvider(t, root, newOpenAPIProvider)
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
	provider := openapitest.NewProvider(t, root, newOpenAPIProvider)
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
	provider := openapitest.NewProvider(t, root, newOpenAPIProvider)
	ctx := context.Background()

	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("tools = %d, want getPet+createPet+listOrders+ping (deletePet denied)", len(tools))
	}
}
