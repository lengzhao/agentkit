package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
)

type nameOnlyTool struct{ name string }

func (t nameOnlyTool) Name() string                          { return t.name }
func (t nameOnlyTool) Description() string                   { return t.name }
func (t nameOnlyTool) InputSchema() agentkit.JSONSchema      { return agentkit.JSONSchema{Type: "object"} }
func (t nameOnlyTool) Call(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

func TestExposedToolName(t *testing.T) {
	t.Parallel()
	if ExposedToolName("ah__codegraph.explore") != "ah__codegraph_explore" {
		t.Fatal("sanitize dotted tool name")
	}
}

func TestBuildExposedCatalogDropsDuplicate(t *testing.T) {
	t.Parallel()
	a := nameOnlyTool{name: "p__a.b"}
	b := nameOnlyTool{name: "p__a-b"}
	exposed, dropped := buildExposedCatalog(nil, map[string]agentkit.Tool{
		a.name: a,
		b.name: b,
	})
	if dropped != 1 || len(exposed) != 1 {
		t.Fatalf("exposed=%d dropped=%d", len(exposed), dropped)
	}
	if _, ok := exposed["p__a_b"]; !ok {
		t.Fatalf("keys=%v", exposed)
	}
}
