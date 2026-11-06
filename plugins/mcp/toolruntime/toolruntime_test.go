package toolruntime_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/mcp/toolruntime"
	"github.com/lengzhao/agentkit/runtime/mcpbridge"
	"github.com/lengzhao/pluginkit"
	"github.com/lengzhao/pluginkit/build"
	"github.com/lengzhao/pluginkit/manager"
)

type runtime struct{}

func (runtime) Visible(context.Context) ([]agentkit.ToolSpec, error) {
	return []agentkit.ToolSpec{{Name: "echo"}}, nil
}

func init() {
	pluginkit.Register("test/mcp-standalone-runtime", func(struct{}, struct{}) (agentkit.ToolRuntime, error) { return runtime{}, nil })
}
func TestPluginGraphWithoutACP(t *testing.T) {
	doc, err := manager.FromYAML([]byte("bridge:\n  use: mcp/tool-runtime\n  deps:\n    runtime: tools\ntools:\n  use: test/mcp-standalone-runtime\n"))
	if err != nil {
		t.Fatal(err)
	}
	bridge, _, err := build.Build[*mcpbridge.Bridge](t.Context(), doc.ToGraph(), "bridge")
	if err != nil {
		t.Fatal(err)
	}
	list, err := bridge.Tools(t.Context())
	if err != nil || len(list) != 1 {
		t.Fatalf("catalog: %+v %v", list, err)
	}
}
func (runtime) Execute(context.Context, agentkit.ToolCall) (agentkit.ToolResult, error) {
	return agentkit.ToolResult{}, nil
}
func TestStandaloneWithoutACPOrWorkspace(t *testing.T) {
	b, err := toolruntime.New(toolruntime.Config{}, toolruntime.Deps{Runtime: runtime{}})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := b.Tools(t.Context())
	if err != nil || len(tools) != 1 {
		t.Fatalf("tools: %+v %v", tools, err)
	}
	if _, err = toolruntime.New(toolruntime.Config{}, toolruntime.Deps{}); err == nil {
		t.Fatal("missing runtime accepted")
	}
}
