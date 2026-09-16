package deferred

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/tools"
)

type stubTool struct {
	name string
}

func (t stubTool) Name() string        { return t.name }
func (t stubTool) Description() string { return t.name + " tool" }
func (t stubTool) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{Type: "object"}
}
func (t stubTool) Call(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

type stubProvider struct {
	tools []agentkit.Tool
}

func (p *stubProvider) ListTools(context.Context) ([]agentkit.Tool, error) {
	return p.tools, nil
}

func TestAutoSkipsSmallDeferrableCatalog(t *testing.T) {
	t.Parallel()
	outer := mustNewRuntime(t, Config{DisclosureConfig: DisclosureConfig{Enabled: EnabledAuto}})
	specs, err := outer.Visible(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		if s.Name == ToolSearch {
			t.Fatalf("auto should not bridge tiny catalog: %v", specs)
		}
	}
}

func TestEagerToolsKeepsNamedDynamic(t *testing.T) {
	t.Parallel()
	deps := tools.RuntimeDeps{
		Tools: []agentkit.Tool{stubTool{name: "read"}},
		DynamicTools: []agentkit.ToolProvider{&stubProvider{tools: []agentkit.Tool{
			stubTool{name: "mcp__ping"},
			stubTool{name: "mcp__keep"},
		}}},
	}
	outer, err := New(Config{
		DisclosureConfig: DisclosureConfig{
			Enabled:    EnabledOn,
			EagerTools: []string{"mcp__keep"},
		},
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	specs, err := outer.Visible(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	if !names["mcp__keep"] {
		t.Fatalf("eagerTools should keep tool visible: %v", names)
	}
	if names["mcp__ping"] {
		t.Fatalf("other dynamic should defer: %v", names)
	}
}

func TestEagerToolsOverridesDeferTools(t *testing.T) {
	t.Parallel()
	split := classify([]agentkit.ToolSpec{
		{Name: "skill"},
	}, map[string]bool{"skill": true}, map[string]bool{"skill": true})
	if len(split.Eager) != 1 || len(split.Deferrable) != 0 {
		t.Fatalf("eager wins over deferTools: %+v", split)
	}
}

func TestEnabledUnmarshalBoolAndString(t *testing.T) {
	t.Parallel()
	var off EnabledSetting
	if err := json.Unmarshal([]byte("false"), &off); err != nil || off != EnabledOff {
		t.Fatalf("off=%q err=%v", off, err)
	}
	var on EnabledSetting
	if err := json.Unmarshal([]byte(`"on"`), &on); err != nil || on != EnabledOn {
		t.Fatalf("on=%q err=%v", on, err)
	}
}

func mustNewRuntime(t *testing.T, cfg Config) agentkit.ToolRuntime {
	outer, err := New(cfg, runtimeDepsForTest())
	if err != nil {
		t.Fatal(err)
	}
	return outer
}

func runtimeDepsForTest() tools.RuntimeDeps {
	return tools.RuntimeDeps{
		Tools: []agentkit.Tool{stubTool{name: "read"}},
		DynamicTools: []agentkit.ToolProvider{&stubProvider{tools: []agentkit.Tool{
			stubTool{name: "mcp__ping"},
		}}},
	}
}
