package mcpbridge_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/mcpbridge"
	mcp "github.com/mark3labs/mcp-go/mcp"
)

type runtime struct {
	specs  []agentkit.ToolSpec
	result agentkit.ToolResult
	err    error
	call   agentkit.ToolCall
	ctx    context.Context
}

func (r *runtime) Visible(context.Context) ([]agentkit.ToolSpec, error) { return r.specs, r.err }
func (r *runtime) Execute(ctx context.Context, c agentkit.ToolCall) (agentkit.ToolResult, error) {
	r.ctx = ctx
	r.call = c
	return r.result, r.err
}
func request(name string, args any) mcp.CallToolRequest {
	var r mcp.CallToolRequest
	r.Params.Name = name
	r.Params.Arguments = args
	return r
}

func TestCatalogUsesRuntimeSchemaAndValidatesNames(t *testing.T) {
	r := &runtime{specs: []agentkit.ToolSpec{{Name: "z", InputSchema: agentkit.JSONSchema{Raw: map[string]any{"type": "object", "additionalProperties": false}}}, {Name: "a"}}}
	b, err := mcpbridge.New(r)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := b.Tools(t.Context())
	if err != nil || len(tools) != 2 || tools[0].Name != "a" {
		t.Fatalf("catalog: %+v %v", tools, err)
	}
	raw, _ := json.Marshal(tools[1])
	var obj map[string]any
	json.Unmarshal(raw, &obj)
	if obj["inputSchema"].(map[string]any)["additionalProperties"] != false {
		t.Fatalf("schema lost: %s", raw)
	}
	r.specs = append(r.specs, r.specs[0])
	if _, err = b.Tools(t.Context()); err == nil {
		t.Fatal("duplicate accepted")
	}
	r.specs = []agentkit.ToolSpec{{Name: " "}}
	if _, err = b.Tools(t.Context()); err == nil {
		t.Fatal("blank accepted")
	}
	if _, err = mcpbridge.New(nil); err == nil {
		t.Fatal("nil runtime accepted")
	}
}
func TestCallsPreserveContextArgumentsAndRuntimeDecisions(t *testing.T) {
	r := &runtime{result: agentkit.ToolResult{Content: "answer"}}
	b, _ := mcpbridge.New(r)
	ctx := context.WithValue(t.Context(), struct{}{}, "trusted")
	out, err := b.CallTool(ctx, request("echo", nil))
	if err != nil || out.IsError {
		t.Fatalf("call: %+v %v", out, err)
	}
	if r.ctx != ctx || r.call.Name != "echo" || r.call.ID == "" || string(r.call.Input) != "{}" {
		t.Fatalf("call lost: %+v", r.call)
	}
	firstID := r.call.ID
	for _, decision := range []string{"deny", "timeout", "error"} {
		r.result.Audit = map[string]string{"decision": decision}
		out, err = b.CallTool(ctx, request("echo", map[string]any{"n": 2}))
		if err != nil || !out.IsError {
			t.Fatalf("decision %s: %+v %v", decision, out, err)
		}
	}
	if r.call.ID == firstID || string(r.call.Input) != "{\"n\":2}" {
		t.Fatalf("identity/args: %+v", r.call)
	}
	r.err = errors.New("backend failed")
	out, err = b.CallTool(ctx, request("echo", nil))
	if !errors.Is(err, r.err) || !out.IsError {
		t.Fatalf("cause lost: %+v %v", out, err)
	}
}
func TestCancelledCallDoesNotExecute(t *testing.T) {
	r := &runtime{}
	b, _ := mcpbridge.New(r)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out, err := b.CallTool(ctx, request("echo", nil))
	if !errors.Is(err, context.Canceled) || !out.IsError || r.call.ID != "" {
		t.Fatalf("cancel: %+v %v", out, err)
	}
}
