package workflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func newTestTool(t *testing.T, root string) agentkit.Tool {
	t.Helper()
	tool, err := NewWorkflow(WorkflowConfig{}, WorkflowDeps{
		Runtime:   &stubToolRuntime{content: `{"imported":true}`},
		Workspace: stubWorkspace{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tool
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

func TestWorkflowToolCallEndToEnd(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "pipe", echoPipeline)
	tool := newTestTool(t, root)
	out, err := tool.Call(context.Background(), json.RawMessage(`{"name":"pipe","args":{"msg":"hi"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != `done: {"imported":true}` {
		t.Fatalf("out = %q", out)
	}
}

func TestWorkflowToolRejectsBadArgs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "pipe", echoPipeline)
	tool := newTestTool(t, root)
	out, _ := tool.Call(context.Background(), json.RawMessage(`{"name":"pipe","args":{}}`))
	if !strings.Contains(out, "inputs schema") {
		t.Fatalf("out = %q", out)
	}
	out, _ = tool.Call(context.Background(), json.RawMessage(`{"name":"missing"}`))
	if !strings.Contains(out, "not found") || !strings.Contains(out, "pipe") {
		t.Fatalf("out = %q", out)
	}
}

func TestWorkflowDescriptionListsPipelines(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "pipe", echoPipeline)
	tool := newTestTool(t, root)
	desc := tool.Description()
	if !strings.Contains(desc, "pipe: echo and import") {
		t.Fatalf("desc = %q", desc)
	}
}

func TestWorkflowCommandListsFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "pipe", echoPipeline)
	tool := newTestTool(t, root)
	cp, ok := tool.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("tool does not implement CommandProvider")
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
