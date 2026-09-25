package deferred_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tools/deferred"
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

func runtimeDeps() tools.RuntimeDeps {
	return tools.RuntimeDeps{
		Tools: []agentkit.Tool{stubTool{name: "read"}},
		DynamicTools: []agentkit.ToolProvider{&stubProvider{tools: []agentkit.Tool{
			stubTool{name: "mcp__ping"},
		}}},
	}
}

func TestPassthroughWhenDisabled(t *testing.T) {
	t.Parallel()
	outer, err := deferred.New(deferred.Config{DisclosureConfig: deferred.DisclosureConfig{Enabled: deferred.EnabledOff}}, runtimeDeps())
	if err != nil {
		t.Fatal(err)
	}
	specs, err := outer.Visible(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("visible len = %d", len(specs))
	}
}

func TestVisibleReplacesDynamicWithBridges(t *testing.T) {
	t.Parallel()
	outer, err := deferred.New(deferred.Config{DisclosureConfig: deferred.DisclosureConfig{Enabled: deferred.EnabledOn}}, runtimeDeps())
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
	if !names["read"] || names["mcp__ping"] {
		t.Fatalf("names = %v", names)
	}
	if !names[deferred.ToolSearch] || !names[deferred.ToolDescribe] || !names[deferred.ToolCall] {
		t.Fatalf("missing bridge tools: %v", names)
	}
}

type abortBeforeToolHooks struct{}

func (abortBeforeToolHooks) BeforeStep(context.Context, *agentkit.BeforeStep) error { return nil }
func (abortBeforeToolHooks) BeforeTool(context.Context, *agentkit.ToolCall) error {
	return agentkit.AbortTurn(errors.New("hook blocked"))
}
func (abortBeforeToolHooks) AfterTool(context.Context, *agentkit.ToolResult) error { return nil }
func (abortBeforeToolHooks) TurnStopping(context.Context, *agentkit.TurnStopping) error { return nil }
func (abortBeforeToolHooks) TurnComplete(context.Context, *agentkit.TurnComplete) error { return nil }

func TestToolCallPropagatesAbortTurn(t *testing.T) {
	t.Parallel()
	deps := runtimeDeps()
	deps.Hooks = abortBeforeToolHooks{}
	outer, err := deferred.New(deferred.Config{DisclosureConfig: deferred.DisclosureConfig{Enabled: deferred.EnabledOn}}, deps)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"calls": []map[string]any{{"name": "mcp__ping", "arguments": map[string]any{}}},
	})
	_, err = outer.Execute(context.Background(), agentkit.ToolCall{
		ID: "1", Name: deferred.ToolCall, Input: payload,
	})
	if !agentkit.IsTurnAbort(err) {
		t.Fatalf("want turn abort, got %v", err)
	}
}

func TestToolCallUnwrapsToInner(t *testing.T) {
	t.Parallel()
	outer, err := deferred.New(deferred.Config{DisclosureConfig: deferred.DisclosureConfig{Enabled: deferred.EnabledOn}}, runtimeDeps())
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"calls": []map[string]any{{"name": "mcp__ping", "arguments": map[string]any{}}},
	})
	result, err := outer.Execute(context.Background(), agentkit.ToolCall{
		ID: "1", Name: deferred.ToolCall, Input: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "mcp__ping" {
		t.Fatalf("result name = %q", result.Name)
	}
	if result.Content != "ok" {
		t.Fatalf("content = %q", result.Content)
	}
}
