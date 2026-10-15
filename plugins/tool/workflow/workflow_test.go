package workflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// 保留字 timeoutSeconds 注入模型侧 schema，供覆盖默认总超时。
	if props[timeoutSecondsArg] == nil {
		t.Fatalf("schema missing reserved %s: %v", timeoutSecondsArg, schema.Raw)
	}
	if !strings.Contains(pipe.Description(), "Default timeout:") {
		t.Fatalf("desc missing default timeout: %q", pipe.Description())
	}

	ping := toolByName(t, ctx, provider, "workflow__ping")
	pingProps, ok := ping.InputSchema().Raw["properties"].(map[string]any)
	if !ok || len(pingProps) != 1 || pingProps[timeoutSecondsArg] == nil {
		t.Fatalf("ping schema should carry only the reserved timeout arg: %v", ping.InputSchema().Raw)
	}
	if ping.InputSchema().Raw["type"] != "object" {
		t.Fatalf("ping schema type = %q", ping.InputSchema().Raw["type"])
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

func TestWorkflowDefaultTimeoutSeconds(t *testing.T) {
	t.Parallel()
	wf := &Workflow{Steps: []Step{
		{ID: "s", Type: StepScript, Run: "echo"},                    // 未设 → 120
		{ID: "t", Type: StepTool, Tool: "x", TimeoutSeconds: 30},    // 30
		{ID: "m", Type: StepModel, Prompt: "p"},                     // 未设 → 120
		{ID: "a", Type: StepAgent, Agent: "x", Task: "y"},           // 未设 → 900
		{ID: "a2", Type: StepAgent, Agent: "x", Task: "y", TimeoutSeconds: 60}, // 60
	}}
	want := 120 + 30 + 120 + 900 + 60 + TimeoutBufferSeconds
	if got := wf.DefaultTimeoutSeconds(); got != want {
		t.Fatalf("DefaultTimeoutSeconds = %d, want %d", got, want)
	}
}

func TestWorkflowRunToolCallTimeout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "pipe", echoPipeline)
	provider := newTestProvider(t, root)
	tool := toolByName(t, context.Background(), provider, "workflow__pipe")

	tp, ok := tool.(agentkit.CallTimeoutProvider)
	if !ok {
		t.Fatal("workflow tool does not implement CallTimeoutProvider")
	}
	// echoPipeline: script(120) + tool(120) + buffer(5)
	if got := tp.CallTimeout(json.RawMessage(`{"msg":"hi"}`)); got != 245*time.Second {
		t.Fatalf("default CallTimeout = %v, want 245s", got)
	}
	if got := tp.CallTimeout(json.RawMessage(`{"msg":"hi","timeoutSeconds":42}`)); got != 42*time.Second {
		t.Fatalf("override CallTimeout = %v, want 42s", got)
	}
	if got := tp.CallTimeout(nil); got != 245*time.Second {
		t.Fatalf("empty input CallTimeout = %v, want 245s", got)
	}
}

func TestWorkflowRunToolStripsReservedTimeoutArg(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// additionalProperties: false —— timeoutSeconds 若漏进引擎侧校验会直接失败。
	writeWorkflow(t, filepath.Join(root, "workflows"), "strict", `{
  "description": "strict inputs",
  "inputs": {"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"],"additionalProperties":false},
  "steps": [{"id":"p","type":"script","run":"echo {{ .inputs.msg | quote }}"}],
  "result": "{{ .steps.p.stdout }}"
}`)
	provider := newTestProvider(t, root)
	tool := toolByName(t, context.Background(), provider, "workflow__strict")
	out, err := tool.Call(context.Background(), json.RawMessage(`{"msg":"hi","timeoutSeconds":30}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Fatalf("out = %q", out)
	}
}

func TestWorkflowInputSchemaDoesNotMutateDeclaredInputs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "pipe", echoPipeline)
	provider := newTestProvider(t, root)
	tool := toolByName(t, context.Background(), provider, "workflow__pipe")
	_ = tool.InputSchema()
	// 保留字只能存在于模型侧 schema 副本，不得污染 workflow 声明的 inputs。
	wt := tool.(*workflowRunTool)
	props, _ := wt.wf.Inputs["properties"].(map[string]any)
	if _, leaked := props[timeoutSecondsArg]; leaked {
		t.Fatalf("reserved arg leaked into declared inputs: %v", wt.wf.Inputs)
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
