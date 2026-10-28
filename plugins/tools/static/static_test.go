package static_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tools/static"
)

type stubTool struct{ name string }

func (s stubTool) Name() string                                          { return s.name }
func (s stubTool) Description() string                                   { return "stub " + s.name }
func (s stubTool) InputSchema() agentkit.JSONSchema                      { return agentkit.JSONSchema{Type: "object"} }
func (s stubTool) Call(context.Context, json.RawMessage) (string, error) { return "ok", nil }

func TestStaticProviderListsConfiguredTools(t *testing.T) {
	t.Parallel()

	p, err := static.New(static.Config{}, static.Deps{Tools: []agentkit.Tool{stubTool{"a"}, nil, stubTool{"b"}}})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := p.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name() != "a" || tools[1].Name() != "b" {
		t.Fatalf("tools = %+v", tools)
	}
}

// A referenced instance may have been pruned; an empty list must not abort the
// whole config build.
func TestStaticProviderAllowsEmptyList(t *testing.T) {
	t.Parallel()

	p, err := static.New(static.Config{}, static.Deps{})
	if err != nil {
		t.Fatalf("empty tool list must be legal: %v", err)
	}
	tools, err := p.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %+v", tools)
	}
}
