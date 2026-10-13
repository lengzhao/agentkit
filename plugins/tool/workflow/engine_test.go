package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
)

type stubWorkspace struct{ root string }

func (s stubWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	rel = strings.TrimPrefix(rel, "local:")
	rel = strings.TrimPrefix(rel, "global:")
	return filepath.Join(s.root, filepath.FromSlash(rel)), nil
}

type stubToolRuntime struct {
	calls   []agentkit.ToolCall
	content string
	err     error
}

func (s *stubToolRuntime) Visible(context.Context) ([]agentkit.ToolSpec, error) { return nil, nil }
func (s *stubToolRuntime) Execute(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	s.calls = append(s.calls, call)
	if s.err != nil {
		return agentkit.ToolResult{}, s.err
	}
	if call.Name == bashToolName {
		return s.runBash(ctx, call)
	}
	return agentkit.ResultFromCall(call, s.content), nil
}

func (s *stubToolRuntime) runBash(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return agentkit.ToolResult{}, err
	}
	cmd := exec.CommandContext(ctx, "bash", "-lc", in.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	text := stdout.String()
	if stderr.Len() > 0 {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "[stderr]\n" + stderr.String()
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			text += fmt.Sprintf("\n\nCommand exited with code %d", exitErr.ExitCode())
			return agentkit.ResultFromCall(call, text), nil
		}
		return agentkit.ToolResult{}, runErr
	}
	return agentkit.ResultFromCall(call, text), nil
}

type stubStream struct {
	text string
	done bool
}

func (s *stubStream) Recv() (agentkit.LLMEvent, error) {
	if s.done {
		return agentkit.LLMEvent{}, io.EOF
	}
	s.done = true
	return agentkit.LLMEvent{
		Type:    agentkit.LLMEventMessage,
		Message: &agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: s.text}}},
	}, nil
}
func (s *stubStream) Close() error { return nil }

type stubLLM struct {
	text      string
	lastModel string
	lastSys   string
}

func (s *stubLLM) Name() string { return "stub" }
func (s *stubLLM) Stream(_ context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	s.lastModel = req.Model
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			s.lastSys = msg.Content[0].Text
		}
	}
	return &stubStream{text: s.text}, nil
}

type stubSpawner struct {
	req subagent.Request
	res subagent.Result
}

func (s *stubSpawner) Definitions(context.Context) ([]subagent.Definition, error) { return nil, nil }
func (s *stubSpawner) Run(_ context.Context, req subagent.Request) (subagent.Result, error) {
	s.req = req
	return s.res, nil
}

func writeWorkflow(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func engineWith(ws stubWorkspace) *Engine { return &Engine{Workspace: ws} }

func TestEngineScriptStepByReference(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wfDir := filepath.Join(root, "workflows")
	writeWorkflow(t, wfDir, "gen", `{
	  "description": "generate and submit",
	  "inputs": {"type":"object","properties":{"date":{"type":"string"}},"required":["date"]},
	  "steps": [
	    {"id":"gen","type":"script","run":"printf '{\"total\": 42}' > {{ .workflow.runDir }}/ledger.json && echo done"},
	    {"id":"submit","type":"tool","tool":"ledger__import","args":{"path":"{{ .workflow.runDir }}/ledger.json"}}
	  ],
	  "result": "submitted: {{ .steps.submit.text }}"
	}`)
	wf, err := Find(context.Background(), stubWorkspace{root}, []string{"local:workflows"}, "gen")
	if err != nil {
		t.Fatal(err)
	}
	rt := &stubToolRuntime{content: `{"status":"ok"}`}
	engine := engineWith(stubWorkspace{root})
	engine.Tools = rt
	out, err := engine.Run(context.Background(), wf, map[string]any{"date": "2026-10-13"})
	if err != nil {
		t.Fatal(err)
	}
	if out != `submitted: {"status":"ok"}` {
		t.Fatalf("result = %q", out)
	}
	var importCall *agentkit.ToolCall
	for i := range rt.calls {
		if rt.calls[i].Name == "ledger__import" {
			importCall = &rt.calls[i]
			break
		}
	}
	if importCall == nil {
		t.Fatalf("ledger__import not called; calls = %+v", rt.calls)
	}
	var args map[string]any
	if err := json.Unmarshal(importCall.Input, &args); err != nil {
		t.Fatal(err)
	}
	path, _ := args["path"].(string)
	if !strings.HasSuffix(path, "ledger.json") {
		t.Fatalf("path arg = %q, want runDir ledger.json", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != `{"total": 42}` {
		t.Fatalf("ledger content = %q", content)
	}
}

func TestEngineScriptSpillWritesArtifact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wfDir := filepath.Join(root, "workflows")
	writeWorkflow(t, wfDir, "big", `{
	  "description": "large output spills",
	  "steps": [
	    {"id":"dump","type":"script","run":"head -c 60000 /dev/zero | tr '\\0' 'x'"},
	    {"id":"send","type":"tool","tool":"up__load","args":{"path":"{{ .steps.dump.path }}"}}
	  ],
	  "result": "path={{ .steps.send.text }}"
	}`)
	wf, err := Find(context.Background(), stubWorkspace{root}, []string{"local:workflows"}, "big")
	if err != nil {
		t.Fatal(err)
	}
	rt := &stubToolRuntime{content: "ok"}
	engine := engineWith(stubWorkspace{root})
	engine.Tools = rt
	if _, err := engine.Run(context.Background(), wf, nil); err != nil {
		t.Fatal(err)
	}
	var path string
	for _, call := range rt.calls {
		if call.Name != "up__load" {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal(call.Input, &args); err != nil {
			t.Fatal(err)
		}
		path, _ = args["path"].(string)
		break
	}
	if path == "" {
		t.Fatal("expected artifact path for spilled output")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 60000 {
		t.Fatalf("artifact size = %d, want full output", info.Size())
	}
}

func TestEngineInputsValidation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wf := &Workflow{
		Name:        "needs",
		Description: "d",
		Inputs:      map[string]any{"type": "object", "properties": map[string]any{"date": map[string]any{"type": "string"}}, "required": []any{"date"}},
		Steps:       []Step{{ID: "s", Type: StepScript, Run: "true"}},
		Result:      "ok",
	}
	engine := engineWith(stubWorkspace{root})
	engine.Tools = &stubToolRuntime{}
	if _, err := engine.Run(context.Background(), wf, map[string]any{}); err == nil {
		t.Fatal("expected inputs validation error")
	} else if !strings.Contains(err.Error(), "inputs schema") {
		t.Fatalf("err = %v", err)
	}
}

func TestEngineToolStepFailureAborts(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "fail", Description: "d",
		Steps: []Step{
			{ID: "a", Type: StepTool, Tool: "x__y"},
			{ID: "b", Type: StepScript, Run: "echo should-not-run"},
		},
		Result: "done",
	}
	rt := &stubToolRuntime{err: errors.New("denied by policy")}
	engine := engineWith(stubWorkspace{t.TempDir()})
	engine.Tools = rt
	_, err := engine.Run(context.Background(), wf, nil)
	if err == nil || !strings.Contains(err.Error(), "denied by policy") {
		t.Fatalf("err = %v", err)
	}
}

func TestEngineContinueOnError(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "cont", Description: "d",
		Steps: []Step{
			{ID: "a", Type: StepScript, Run: "exit 3", ContinueOnError: true},
			{ID: "b", Type: StepScript, Run: "echo fine"},
		},
		Result: "a={{ .steps.a.status }} b={{ .steps.b.status }}",
	}
	engine := engineWith(stubWorkspace{t.TempDir()})
	engine.Tools = &stubToolRuntime{}
	out, err := engine.Run(context.Background(), wf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "a=error b=ok" {
		t.Fatalf("out = %q", out)
	}
}

func TestEngineModelStepSchema(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "conv", Description: "d",
		Steps: []Step{
			{ID: "c", Type: StepModel, Model: "fast", Prompt: "convert",
				OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []any{"ok"}}},
		},
		Result: `ok={{ dig .steps.c.json "ok" }}`,
	}
	llm := &stubLLM{text: `{"ok": true}`}
	engine := engineWith(stubWorkspace{t.TempDir()})
	engine.LLM = llm
	out, err := engine.Run(context.Background(), wf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok=true" {
		t.Fatalf("out = %q", out)
	}
	if llm.lastModel != "fast" || !strings.Contains(llm.lastSys, "JSON Schema") {
		t.Fatalf("model=%q sys=%q", llm.lastModel, llm.lastSys)
	}
}

func TestEngineModelStepSchemaViolation(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "conv", Description: "d",
		Steps: []Step{
			{ID: "c", Type: StepModel, Prompt: "convert",
				OutputSchema: map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []any{"ok"}}},
		},
		Result: "never",
	}
	engine := engineWith(stubWorkspace{t.TempDir()})
	engine.LLM = &stubLLM{text: `{"ok": "not-a-bool"}`}
	_, err := engine.Run(context.Background(), wf, nil)
	if err == nil || !strings.Contains(err.Error(), "outputSchema") {
		t.Fatalf("err = %v", err)
	}
}

func TestEngineModelStepInputCap(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "cap", Description: "d",
		Steps: []Step{
			{ID: "c", Type: StepModel, Prompt: strings.Repeat("x", 200), MaxInputBytes: 100},
		},
		Result: "never",
	}
	engine := engineWith(stubWorkspace{t.TempDir()})
	engine.LLM = &stubLLM{text: "{}"}
	_, err := engine.Run(context.Background(), wf, nil)
	if err == nil || !strings.Contains(err.Error(), "maxInputBytes") {
		t.Fatalf("err = %v", err)
	}
}

func TestEngineAgentStepForcedSync(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "audit", Description: "d",
		Steps: []Step{
			{ID: "a", Type: StepAgent, Agent: "auditor", Task: "review {{ .inputs.path }}", TimeoutSeconds: 30},
		},
		Result: "{{ .steps.a.text }}",
	}
	spawner := &stubSpawner{res: subagent.Result{Agent: "auditor", Status: subagent.StatusCompleted, Summary: "clean", Session: "sub:1"}}
	engine := engineWith(stubWorkspace{t.TempDir()})
	engine.Subagent = spawner
	out, err := engine.Run(context.Background(), wf, map[string]any{"path": "/tmp/x"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "clean" {
		t.Fatalf("out = %q", out)
	}
	if spawner.req.Async == nil || *spawner.req.Async {
		t.Fatalf("async = %v, want forced false", spawner.req.Async)
	}
	if spawner.req.TimeoutSeconds == nil || *spawner.req.TimeoutSeconds != 30 {
		t.Fatalf("timeout = %v", spawner.req.TimeoutSeconds)
	}
}

func TestDiscoverLocalWinsAndBadFileSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkflow(t, filepath.Join(root, "workflows"), "dup", `{"description":"local","steps":[{"id":"s","type":"script","run":"true"}],"result":"local"}`)
	writeWorkflow(t, filepath.Join(root, "global", "workflows"), "dup", `{"description":"global","steps":[{"id":"s","type":"script","run":"true"}],"result":"global"}`)
	if err := os.WriteFile(filepath.Join(root, "workflows", "broken.json"), []byte(`{"description":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	found, errs := Discover(context.Background(), stubWorkspace{root}, []string{"local:workflows", "global:workflows"})
	if len(found) != 1 || found[0].Description != "local" {
		t.Fatalf("found = %+v", found)
	}
	if len(errs) == 0 {
		t.Fatal("expected parse error for broken.json")
	}
}

func TestEngineRunRecordsStepStatesForResult(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "st", Description: "d",
		Steps: []Step{
			{ID: "one", Type: StepScript, Run: "echo hello"},
			{ID: "two", Type: StepScript, Run: "echo world"},
		},
		Result: `{{ trim .steps.one.stdout }}-{{ trim .steps.two.stdout }}`,
	}
	engine := engineWith(stubWorkspace{t.TempDir()})
	engine.Tools = &stubToolRuntime{}
	// trim is not in the funcmap; expect a template error to surface.
	if _, err := engine.Run(context.Background(), wf, nil); err == nil {
		t.Fatal("expected template error for unknown function")
	}
	wf.Result = fmt.Sprintf("%s", `{{ .steps.one.stdout }}{{ .steps.two.stdout }}`)
	out, err := engine.Run(context.Background(), wf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello\nworld\n" {
		t.Fatalf("out = %q", out)
	}
}
