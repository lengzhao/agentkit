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
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
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

// discoverOne loads the single workflow under <root>/workflows.
func discoverOne(t *testing.T, root string) *Workflow {
	t.Helper()
	wfs, errs := Discover(context.Background(), stubWorkspace{root}, []string{"local:workflows"})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	if len(wfs) != 1 {
		t.Fatalf("workflows = %d, want 1", len(wfs))
	}
	return wfs[0]
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

// testShell runs commands with bash -lc and returns raw output (StructuredShell).
type testShell struct{}

func (testShell) RunStructured(ctx context.Context, command string, _ *float64) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return "", "", 0, runErr
		}
	}
	return stdout.String(), stderr.String(), exitCode, nil
}

func engineWith(ws stubWorkspace) *Engine { return &Engine{Workspace: ws, Shell: testShell{}} }

func TestEngineStepEmitsToolCallAndResult(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wfDir := filepath.Join(root, "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wf := &Workflow{
		Name: "emit", Description: "d", Dir: wfDir,
		Steps: []Step{
			{ID: "one", Type: StepScript, Run: "echo emitted"},
		},
		Result: "{{ .steps.one.stdout }}",
	}
	var events []agentkit.OutboundEvent
	emit := agentkit.OutboundEmit(func(_ context.Context, ev agentkit.OutboundEvent) error {
		events = append(events, ev)
		return nil
	})
	ctx := rctx.ContextWithOutboundEmit(context.Background(), emit)
	engine := engineWith(stubWorkspace{root})
	if _, err := engine.Run(ctx, wf, nil); err != nil {
		t.Fatal(err)
	}
	var toolCalls, toolResults int
	for _, ev := range events {
		if ev.Type == agentkit.EventToolCall {
			toolCalls++
		}
		if ev.Type == agentkit.EventToolResult {
			toolResults++
		}
	}
	if toolCalls < 1 || toolResults < 1 {
		t.Fatalf("expected at least one tool/call and one tool/result, got %d call(s) %d result(s)", toolCalls, toolResults)
	}
}

func TestEngineScriptFallbackToBashTool(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wfDir := filepath.Join(root, "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wf := &Workflow{
		Name: "fb", Description: "d", Dir: wfDir,
		Steps: []Step{
			{ID: "one", Type: StepScript, Run: "echo fallback"},
		},
		Result: "{{ .steps.one.stdout }}",
	}
	rt := &stubToolRuntime{}
	engine := &Engine{Workspace: stubWorkspace{root}, Tools: rt} // Shell nil → fallback path
	out, err := engine.Run(context.Background(), wf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fallback") {
		t.Fatalf("out = %q", out)
	}
	var bashCall *agentkit.ToolCall
	for i := range rt.calls {
		if rt.calls[i].Name == bashToolName {
			bashCall = &rt.calls[i]
			break
		}
	}
	if bashCall == nil {
		t.Fatal("expected bash tool call when deps.shell is not set")
	}
}

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
	wf := discoverOne(t, root)
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

func TestEngineToolStepInjectsObjectPayload(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	wfDir := filepath.Join(root, "workflows")
	const big = "9007199254740993"
	writeWorkflow(t, wfDir, "update", `{
	  "description": "inject dataset object",
	  "steps": [
	    {"id":"emit","type":"script","run":"printf '%s' '{\"count\":42,\"id\":`+big+`}' > {{ .workflow.runDir }}/payload.json && cat {{ .workflow.runDir }}/payload.json"},
	    {"id":"update","type":"tool","tool":"capitaldesk_update_dataset","args":{
	      "datasetKey":"dailyData",
	      "expectedVersion": 3,
	      "note":"audit",
	      "payload":"{{ readJSON (printf \"%s/payload.json\" .workflow.runDir) }}"
	    }}
	  ],
	  "result": "id={{ dig .steps.emit.json \"id\" }}"
	}`)
	wf := discoverOne(t, root)
	rt := &stubToolRuntime{content: `{"status":"ok"}`}
	engine := engineWith(stubWorkspace{root})
	engine.Tools = rt
	out, err := engine.Run(context.Background(), wf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "id="+big {
		t.Fatalf("result = %q", out)
	}
	var update *agentkit.ToolCall
	for i := range rt.calls {
		if rt.calls[i].Name == "capitaldesk_update_dataset" {
			update = &rt.calls[i]
			break
		}
	}
	if update == nil {
		t.Fatalf("tool not called; calls = %+v", rt.calls)
	}
	text := string(update.Input)
	if strings.Contains(text, `"payload":"`) {
		t.Fatalf("payload encoded as string: %s", text)
	}
	if !strings.Contains(text, `"id":`+big) {
		t.Fatalf("integer literal lost: %s", text)
	}
	if !strings.Contains(text, `"expectedVersion":3`) {
		t.Fatalf("expectedVersion = %s", text)
	}
	dec := json.NewDecoder(bytes.NewReader(update.Input))
	dec.UseNumber()
	var args map[string]any
	if err := dec.Decode(&args); err != nil {
		t.Fatal(err)
	}
	payload, ok := args["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T in %s", args["payload"], text)
	}
	if payload["id"] != json.Number(big) || payload["count"] != json.Number("42") {
		t.Fatalf("payload = %#v", payload)
	}
	if _, isFloat := payload["id"].(float64); isFloat {
		t.Fatal("payload.id is float64")
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
	wf := discoverOne(t, root)
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

// swallowTimeoutRuntime mirrors runtime/tools: an expired call deadline becomes
// the benign timeout sentinel result with err=nil.
type swallowTimeoutRuntime struct{ stubToolRuntime }

func (s *swallowTimeoutRuntime) Execute(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	if strings.Contains(string(call.Input), "sleep") {
		<-ctx.Done()
		return agentkit.ToolResultFromTimeout(call), nil
	}
	return s.stubToolRuntime.Execute(ctx, call)
}

func TestEngineScriptStepTimeoutSurfacesAsError(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "wf-timeout", Description: "d",
		Steps: []Step{
			{ID: "slow", Type: StepScript, Run: "sleep 30", TimeoutSeconds: 1, ContinueOnError: true},
			{ID: "after", Type: StepScript, Run: "echo ok"},
		},
		Result: "slow={{ .steps.slow.status }} err={{ .steps.slow.error }} after={{ .steps.after.status }}",
	}
	engine := &Engine{Workspace: stubWorkspace{t.TempDir()}, Tools: &swallowTimeoutRuntime{}}
	out, err := engine.Run(context.Background(), wf, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "slow=error err=" + agentkit.ToolTimeoutResultText + " after=ok"
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestEngineToolStepRuntimeTimeoutSentinelFails(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "wf-tool-timeout", Description: "d",
		Steps:  []Step{{ID: "call", Type: StepTool, Tool: "slow__tool"}},
		Result: "call={{ .steps.call.status }}",
	}
	engine := &Engine{
		Workspace: stubWorkspace{t.TempDir()},
		Tools:     &stubToolRuntime{content: agentkit.ToolTimeoutResultText},
	}
	_, err := engine.Run(context.Background(), wf, nil)
	if err == nil || !strings.Contains(err.Error(), agentkit.ToolTimeoutResultText) {
		t.Fatalf("err = %v", err)
	}
}

func TestBashStepErrorRecognizesRuntimeTimeoutSentinel(t *testing.T) {
	t.Parallel()
	if got := bashStepError(agentkit.ToolTimeoutResultText); got == "" {
		t.Fatal("bashStepError did not flag the runtime timeout sentinel")
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

func TestEngineModelStepRecordsGeneration(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "gen", Description: "d",
		Steps: []Step{
			{ID: "m", Type: StepModel, Model: "stub-model", Prompt: "say hi"},
		},
		Result: "{{ .steps.m.text }}",
	}
	rec := &rttelemetry.RecordingExporter{}
	tk, err := rttelemetry.NewToolkit(struct{}{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := rttelemetry.WithExporter(context.Background(), rec)
	engine := engineWith(stubWorkspace{t.TempDir()})
	engine.LLM = &stubLLM{text: "hi"}
	engine.Telemetry = tk
	if _, err := engine.Run(ctx, wf, nil); err != nil {
		t.Fatal(err)
	}
	_, observations, _ := rec.Snapshot()
	var step, gen *rttelemetry.RecordedObservation
	for i := range observations {
		switch observations[i].Meta.Name {
		case "workflow.step":
			step = &observations[i]
		case "llm.generation":
			gen = &observations[i]
		}
	}
	if step == nil || gen == nil {
		t.Fatalf("expected workflow.step and llm.generation, got %+v", observations)
	}
	if gen.ParentID != step.ID {
		t.Fatalf("llm.generation parent = %q, want %q (workflow.step)", gen.ParentID, step.ID)
	}
	if gen.Meta.Kind != captelemetry.KindGeneration {
		t.Fatalf("generation kind = %q", gen.Meta.Kind)
	}
	if gen.Meta.Model != "stub-model" {
		t.Fatalf("generation model = %q", gen.Meta.Model)
	}
	if len(gen.Meta.GenerationMessages) == 0 {
		t.Fatal("generation input messages empty")
	}
	if gen.End.Output != "hi" {
		t.Fatalf("generation output = %q", gen.End.Output)
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

func TestEngineWhenSkipsStep(t *testing.T) {
	t.Parallel()
	newWF := func(when string) *Workflow {
		return &Workflow{
			Name: "cond", Description: "d",
			Steps: []Step{
				{ID: "gen", Type: StepScript, Run: "echo data"},
				{ID: "gated", Type: StepScript, Run: "echo should-not-print", When: when},
			},
			Result: "gated={{ .steps.gated.status }} view={{ .steps.gated.stdout }}",
		}
	}
	engine := engineWith(stubWorkspace{t.TempDir()})
	out, err := engine.Run(context.Background(), newWF(`{{ eq .inputs.mode "run" }}`), map[string]any{"mode": "skip"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "gated=skipped view=" {
		t.Fatalf("out = %q", out)
	}
	out, err = engine.Run(context.Background(), newWF(`{{ eq .inputs.mode "run" }}`), map[string]any{"mode": "run"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "gated=ok view=should-not-print\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestEngineWhenOnPreviousStepStatus(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "gate-on-status", Description: "d",
		Steps: []Step{
			{ID: "a", Type: StepScript, Run: "exit 1", ContinueOnError: true},
			{ID: "b", Type: StepScript, Run: "echo recovered", When: `{{ eq .steps.a.status "error" }}`},
			{ID: "c", Type: StepScript, Run: "echo no", When: `{{ eq .steps.a.status "ok" }}`},
		},
		Result: "a={{ .steps.a.status }} b={{ .steps.b.status }} c={{ .steps.c.status }}",
	}
	engine := engineWith(stubWorkspace{t.TempDir()})
	out, err := engine.Run(context.Background(), wf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "a=error b=ok c=skipped" {
		t.Fatalf("out = %q", out)
	}
}

func TestEngineWhenTemplateErrorFailsStep(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "bad-when", Description: "d",
		Steps: []Step{
			{ID: "a", Type: StepScript, Run: "echo x", When: `{{ dig .steps.nope.json "x" }}`},
		},
		Result: "never",
	}
	engine := engineWith(stubWorkspace{t.TempDir()})
	_, err := engine.Run(context.Background(), wf, nil)
	if err == nil || !strings.Contains(err.Error(), "when condition") {
		t.Fatalf("err = %v", err)
	}
}

func TestEngineDAGRunsLevelConcurrently(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "dag", Description: "d",
		Steps: []Step{
			{ID: "a", Type: StepScript, Run: `touch "{{ .workflow.runDir }}/a.started"; until [ -f "{{ .workflow.runDir }}/b.started" ]; do sleep 0.01; done; echo A`},
			// 显式空 dependsOn 断开隐式串行边，与 a 同层并行。
			{ID: "b", Type: StepScript, Run: `touch "{{ .workflow.runDir }}/b.started"; until [ -f "{{ .workflow.runDir }}/a.started" ]; do sleep 0.01; done; echo B`, DependsOn: []string{}},
			{ID: "join", Type: StepScript, DependsOn: []string{"a", "b"},
				Run: `echo "{{ .steps.a.stdout }}{{ .steps.b.stdout }}"`},
		},
		Result: "{{ .steps.join.stdout }}",
	}
	engine := engineWith(stubWorkspace{t.TempDir()})
	// Each sibling must start before the other can finish. A serial executor
	// hits the deadline instead of relying on wall-clock performance thresholds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := engine.Run(ctx, wf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "A\nB\n\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestEngineDAGFailureAbortsLaterLevels(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mark := func(name string) string {
		return filepath.Join(root, name+".ran")
	}
	wf := &Workflow{
		Name: "abort", Description: "d", Dir: root,
		Steps: []Step{
			{ID: "bad", Type: StepScript, Run: "exit 1"},
			{ID: "side", Type: StepScript, DependsOn: []string{}, Run: "touch " + mark("side")},
			{ID: "after", Type: StepScript, DependsOn: []string{"bad"}, Run: "touch " + mark("after")},
		},
		Result: "never",
	}
	engine := engineWith(stubWorkspace{root})
	_, err := engine.Run(context.Background(), wf, nil)
	if err == nil || !strings.Contains(err.Error(), `step "bad"`) {
		t.Fatalf("err = %v", err)
	}
	// 同层 sibling 跑完；失败中止的是下一层。
	if _, statErr := os.Stat(mark("side")); statErr != nil {
		t.Fatalf("same-level sibling did not finish: %v", statErr)
	}
	if _, statErr := os.Stat(mark("after")); !os.IsNotExist(statErr) {
		t.Fatal("dependent level ran despite the abort")
	}
}

func TestEngineSkippedDependencyDoesNotBlockDependent(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "skip-dep", Description: "d",
		Steps: []Step{
			{ID: "maybe", Type: StepScript, Run: "echo x", When: "false"},
			{ID: "downstream", Type: StepScript, Run: "echo ran"},
		},
		Result: "maybe={{ .steps.maybe.status }} downstream={{ .steps.downstream.status }}",
	}
	engine := engineWith(stubWorkspace{t.TempDir()})
	out, err := engine.Run(context.Background(), wf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "maybe=skipped downstream=ok" {
		t.Fatalf("out = %q", out)
	}
}

func TestWorkflowValidateDependsOnAndWhen(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		steps   string
		wantErr string
	}{
		{"unknown dep", `[{"id":"a","type":"script","run":"true","dependsOn":["ghost"]}]`, "matches no step id"},
		{"self dep", `[{"id":"a","type":"script","run":"true","dependsOn":["a"]}]`, "itself"},
		{"duplicate dep", `[{"id":"a","type":"script","run":"true"},{"id":"b","type":"script","run":"true","dependsOn":["a","a"]}]`, "duplicate dependsOn"},
		{"cycle", `[{"id":"a","type":"script","run":"true","dependsOn":["b"]},{"id":"b","type":"script","run":"true","dependsOn":["a"]}]`, "cycle"},
		{"backward dep cycle", `[{"id":"a","type":"script","run":"true","dependsOn":["b"]},{"id":"b","type":"script","run":"true"}]`, "cycle"},
		{"bad when", `[{"id":"a","type":"script","run":"true","when":"{{"}]`, "invalid when template"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"description":"d","steps":` + tc.steps + `,"result":"ok"}`
			_, err := Parse("case", []byte(body))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
	// 合法 DAG：fan-out + join 通过校验。
	body := `{"description":"d","steps":[
	  {"id":"a","type":"script","run":"true"},
	  {"id":"b","type":"script","run":"true","dependsOn":[]},
	  {"id":"c","type":"script","run":"true","dependsOn":["a","b"],"when":"{{ eq .steps.a.status \"ok\" }}"}
	],"result":"ok"}`
	if _, err := Parse("ok", []byte(body)); err != nil {
		t.Fatalf("valid DAG rejected: %v", err)
	}
}

func TestWorkflowDefaultTimeoutCriticalPath(t *testing.T) {
	t.Parallel()
	wf := &Workflow{Steps: []Step{
		{ID: "a", Type: StepScript, Run: "echo", TimeoutSeconds: 120},
		{ID: "b", Type: StepScript, Run: "echo", TimeoutSeconds: 30, DependsOn: []string{}},
		{ID: "c", Type: StepScript, Run: "echo", TimeoutSeconds: 60, DependsOn: []string{"a", "b"}},
	}}
	// 关键路径 max(120, 30) + 60 + 5；串行求和会是 215。
	want := 120 + 60 + TimeoutBufferSeconds
	if got := wf.DefaultTimeoutSeconds(); got != want {
		t.Fatalf("DefaultTimeoutSeconds = %d, want %d", got, want)
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

func TestEngineStepSpanParentsUnderToolObservation(t *testing.T) {
	t.Parallel()
	wf := &Workflow{
		Name: "obs", Description: "d",
		Steps: []Step{
			{ID: "one", Type: StepScript, Run: "echo nested"},
		},
		Result: "{{ .steps.one.stdout }}",
	}
	rec := &rttelemetry.RecordingExporter{}
	tk, err := rttelemetry.NewToolkit(struct{}{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := rttelemetry.WithExporter(context.Background(), rec)
	// Simulate the tool.workflow span the tools runtime opens around Execute.
	ctx, endOuter := tk.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name: "tool.workflow",
		Kind: captelemetry.KindTool,
	})
	engine := engineWith(stubWorkspace{t.TempDir()})
	engine.Telemetry = tk
	if _, err := engine.Run(ctx, wf, nil); err != nil {
		t.Fatal(err)
	}
	endOuter(captelemetry.ObservationEnd{})

	_, observations, _ := rec.Snapshot()
	var outer, run, step *rttelemetry.RecordedObservation
	for i := range observations {
		switch observations[i].Meta.Name {
		case "tool.workflow":
			outer = &observations[i]
		case "workflow.run":
			run = &observations[i]
		case "workflow.step":
			step = &observations[i]
		}
	}
	if outer == nil {
		t.Fatal("tool.workflow observation not recorded")
	}
	if run == nil {
		t.Fatal("workflow.run span not recorded")
	}
	if step == nil {
		t.Fatal("workflow.step span not recorded")
	}
	if run.ParentID != outer.ID {
		t.Fatalf("workflow.run parent = %q, want %q (tool.workflow)", run.ParentID, outer.ID)
	}
	if run.Meta.Input == "" {
		t.Fatal("workflow.run input is empty")
	}
	if run.End.Output == "" {
		t.Fatal("workflow.run output is empty")
	}
	if step.ParentID != run.ID {
		t.Fatalf("workflow.step parent = %q, want %q (workflow.run)", step.ParentID, run.ID)
	}
	if !step.Meta.Scope {
		t.Fatal("workflow.step span must set Scope so nested events/generations parent under it")
	}
	if step.Meta.Input == "" {
		t.Fatal("workflow.step input is empty")
	}
	if step.End.Output == "" {
		t.Fatal("workflow.step output is empty")
	}
	var script *rttelemetry.RecordedObservation
	for i := range observations {
		if observations[i].Meta.Name == "workflow.script" {
			script = &observations[i]
		}
	}
	if script == nil {
		t.Fatal("workflow.script span not recorded")
	}
	if script.ParentID != step.ID {
		t.Fatalf("workflow.script parent = %q, want %q (workflow.step)", script.ParentID, step.ID)
	}
	if script.Meta.Input == "" || script.End.Output == "" {
		t.Fatal("workflow.script input/output is empty")
	}
}
