package workflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func newTestProvider(t *testing.T, root string) agentkit.ToolProvider {
	t.Helper()
	provider, err := NewWorkflow(WorkflowConfig{}, WorkflowDeps{
		Runtime:   &stubToolRuntime{content: `{"imported":true}`},
		Workspace: stubWorkspace{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func toolByName(t *testing.T, ctx context.Context, provider agentkit.ToolProvider, name string) agentkit.Tool {
	t.Helper()
	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	t.Fatalf("tool %q not found; available: %s", name, strings.Join(names, ", "))
	return nil
}

const echoPipeline = `{
  "description": "echo and import",
  "inputs": {"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]},
  "steps": [
    {"id":"e","type":"script","run":"echo {{ .inputs.msg | quote }}"},
    {"id":"i","type":"tool","tool":"ledger__import","args":{"note":"{{ .steps.e.stdout }}"}}
  ],
  "result": "done: {{ .steps.i.text }}"
}`

func TestProviderListsOneToolPerWorkflow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wfDir := filepath.Join(root, "workflows")
	writeWorkflow(t, wfDir, "pipe", echoPipeline)
	writeWorkflow(t, wfDir, "ping", `{
  "description": "no inputs",
  "steps": [{"id":"p","type":"script","run":"echo pong"}],
  "result": "{{ .steps.p.stdout }}"
}`)
	provider := newTestProvider(t, root)
	ctx := context.Background()

	pipe := toolByName(t, ctx, provider, "workflow__pipe")
	if !strings.Contains(pipe.Description(), "echo and import") {
		t.Fatalf("desc = %q", pipe.Description())
	}
	// 模型编辑 workflow 需要文件绝对路径。
	if !strings.Contains(pipe.Description(), filepath.Join(wfDir, "pipe.json")) {
		t.Fatalf("desc missing workflow path: %q", pipe.Description())
	}
	schema := pipe.InputSchema()
	if len(schema.Raw) == 0 {
		t.Fatal("pipe schema should carry the workflow inputs as Raw")
	}
	props, ok := schema.Raw["properties"].(map[string]any)
	if !ok || props["msg"] == nil {
		t.Fatalf("schema Raw = %v", schema.Raw)
	}

	ping := toolByName(t, ctx, provider, "workflow__ping")
	if len(ping.InputSchema().Raw) != 0 {
		t.Fatalf("ping has no inputs, schema Raw should be empty: %v", ping.InputSchema().Raw)
	}
	if ping.InputSchema().Type != "object" {
		t.Fatalf("ping schema type = %q", ping.InputSchema().Type)
	}
}

func TestWorkflowRunToolCallEndToEnd(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "pipe", echoPipeline)
	provider := newTestProvider(t, root)
	tool := toolByName(t, context.Background(), provider, "workflow__pipe")
	out, err := tool.Call(context.Background(), json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != `done: {"imported":true}` {
		t.Fatalf("out = %q", out)
	}
}

func TestWorkflowRunToolRejectsBadArgs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "pipe", echoPipeline)
	provider := newTestProvider(t, root)
	tool := toolByName(t, context.Background(), provider, "workflow__pipe")
	out, _ := tool.Call(context.Background(), json.RawMessage(`{}`))
	if !strings.Contains(out, "inputs schema") {
		t.Fatalf("out = %q", out)
	}
}

func TestWorkflowToolNameSanitized(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "my flow.v2", `{
  "description": "odd name",
  "steps": [{"id":"p","type":"script","run":"echo ok"}],
  "result": "{{ .steps.p.stdout }}"
}`)
	provider := newTestProvider(t, root)
	tools, err := provider.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %d", len(tools))
	}
	name := tools[0].Name()
	if !strings.HasPrefix(name, "workflow__") {
		t.Fatalf("name = %q", name)
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		if !ok {
			t.Fatalf("name %q contains invalid rune %q", name, r)
		}
	}
}

func TestProviderDiscoversEditsNextCall(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wfDir := filepath.Join(root, "workflows")
	writeWorkflow(t, wfDir, "pipe", echoPipeline)
	provider := newTestProvider(t, root)
	ctx := context.Background()
	tools, err := provider.ListTools(ctx)
	if err != nil || len(tools) != 1 {
		t.Fatalf("tools = %v err = %v", tools, err)
	}
	writeWorkflow(t, wfDir, "extra", `{
  "description": "added later",
  "steps": [{"id":"p","type":"script","run":"echo ok"}],
  "result": "{{ .steps.p.stdout }}"
}`)
	tools, err = provider.ListTools(ctx)
	if err != nil || len(tools) != 2 {
		t.Fatalf("tools = %v err = %v", tools, err)
	}
}

func TestWorkflowCommandListsFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "pipe", echoPipeline)
	provider := newTestProvider(t, root)
	cp, ok := provider.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("provider does not implement CommandProvider")
	}
	cmds := cp.Commands()
	if len(cmds) != 1 || cmds[0].Name() != "workflow" {
		t.Fatalf("commands = %v", cmds)
	}
	out, err := cmds[0].CommandExec(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "pipe") || !strings.Contains(out, "2 steps") {
		t.Fatalf("out = %q", out)
	}
}
