package agent_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/runtime/tools"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type staggeredTool struct {
	name  string
	delay time.Duration
}

func (t staggeredTool) Name() string {
	if t.name != "" {
		return t.name
	}
	return "staggered"
}
func (staggeredTool) Description() string { return "sleeps then returns" }
func (staggeredTool) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{Type: "object"}
}
func (t staggeredTool) Call(ctx context.Context, _ json.RawMessage) (string, error) {
	select {
	case <-time.After(t.delay):
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestRunTurnExecutesParallelSafeToolsConcurrently(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{
		{ToolCalls: []agentkit.ToolCall{
			{ID: "a", Name: "staggered", Input: json.RawMessage(`{}`)},
			{ID: "b", Name: "staggered", Input: json.RawMessage(`{}`)},
		}},
		{Text: "done"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.Tool{staggeredTool{delay: 400 * time.Millisecond}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "test"}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID("test:parallel-tools")
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(sessionID),
		Workspace:    string(sessionID),
	})
	start := time.Now()
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 700*time.Millisecond {
		t.Fatalf("expected parallel tool batch (~400ms), took %v", elapsed)
	}
}

type countingSlowTool struct {
	started atomic.Int32
}

func (*countingSlowTool) Name() string        { return "count_slow" }
func (*countingSlowTool) Description() string { return "count concurrent starts" }
func (*countingSlowTool) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{Type: "object"}
}
func (c *countingSlowTool) Call(ctx context.Context, _ json.RawMessage) (string, error) {
	c.started.Add(1)
	select {
	case <-time.After(200 * time.Millisecond):
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestRunTurnParallelToolBatchOverlapsExecution(t *testing.T) {
	t.Parallel()
	tool := &countingSlowTool{}
	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{
		{ToolCalls: []agentkit.ToolCall{
			{ID: "1", Name: "count_slow", Input: json.RawMessage(`{}`)},
			{ID: "2", Name: "count_slow", Input: json.RawMessage(`{}`)},
		}},
		{Text: "done"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{Tools: []agentkit.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "test"}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID("test:overlap")
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(sessionID),
		Workspace:    string(sessionID),
	})
	start := time.Now()
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if time.Since(start) > 350*time.Millisecond {
		t.Fatalf("tools did not overlap enough: %v", time.Since(start))
	}
	if tool.started.Load() != 2 {
		t.Fatalf("started = %d", tool.started.Load())
	}
}

type toolBatchConcurrencyProbe struct {
	state *toolBatchProbeState
	delay time.Duration
}

type toolBatchProbeState struct {
	inFlight      atomic.Int32
	maxConcurrent atomic.Int32
}

func (toolBatchConcurrencyProbe) Name() string        { return "probe" }
func (toolBatchConcurrencyProbe) Description() string { return "tracks overlap" }
func (toolBatchConcurrencyProbe) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{Type: "object"}
}

func (p toolBatchConcurrencyProbe) begin() {
	cur := p.state.inFlight.Add(1)
	for {
		max := p.state.maxConcurrent.Load()
		if cur <= max {
			return
		}
		if p.state.maxConcurrent.CompareAndSwap(max, cur) {
			return
		}
	}
}

func (p toolBatchConcurrencyProbe) Call(ctx context.Context, _ json.RawMessage) (string, error) {
	p.begin()
	defer p.state.inFlight.Add(-1)
	select {
	case <-time.After(p.delay):
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestRunTurnSequentialToolInBatchRunsSerially(t *testing.T) {
	t.Parallel()

	const delay = 120 * time.Millisecond
	probe := toolBatchConcurrencyProbe{
		state: &toolBatchProbeState{},
		delay: delay,
	}
	seqGate, err := agentkit.NewTool[struct{}, string]("seq_gate", func(ctx context.Context, _ struct{}) (string, error) {
		select {
		case <-time.After(delay):
			return "ok", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}).Description("sequential gate").Sequential().Build()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{
		{ToolCalls: []agentkit.ToolCall{
			{ID: "1", Name: "probe", Input: json.RawMessage(`{}`)},
			{ID: "2", Name: "seq_gate", Input: json.RawMessage(`{}`)},
			{ID: "3", Name: "probe", Input: json.RawMessage(`{}`)},
		}},
		{Text: "done"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.Tool{probe, seqGate},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "test"}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID("test:serial-batch")
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(sessionID),
		Workspace:    string(sessionID),
	})
	start := time.Now()
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if max := probe.state.maxConcurrent.Load(); max != 1 {
		t.Fatalf("expected serial batch (max concurrent 1), got %d", max)
	}
	elapsed := time.Since(start)
	if elapsed < 3*delay {
		t.Fatalf("expected ~serial wall time (>= %v), took %v", 3*delay, elapsed)
	}
}

func TestRunTurnDefaultSequentialToolNameForcesSerialBatch(t *testing.T) {
	t.Parallel()

	const delay = 100 * time.Millisecond
	probe := toolBatchConcurrencyProbe{
		state: &toolBatchProbeState{},
		delay: delay,
	}
	// Name "bash" is sequential by default in runtime/tools (no .Sequential() on plugin).
	bashStub := staggeredTool{name: "bash", delay: delay} // default-sequential tool name

	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{
		{ToolCalls: []agentkit.ToolCall{
			{ID: "1", Name: "probe", Input: json.RawMessage(`{}`)},
			{ID: "2", Name: "bash", Input: json.RawMessage(`{}`)},
			{ID: "3", Name: "probe", Input: json.RawMessage(`{}`)},
		}},
		{Text: "done"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.Tool{probe, bashStub},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "test"}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID("test:bash-serial-batch")
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(sessionID),
		Workspace:    string(sessionID),
	})
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if max := probe.state.maxConcurrent.Load(); max != 1 {
		t.Fatalf("expected serial batch when bash in batch, max concurrent = %d", max)
	}
}

type invokeCountTool struct {
	n *atomic.Int32
}

func (invokeCountTool) Name() string        { return "invoke_count" }
func (invokeCountTool) Description() string { return "counts invocations" }
func (invokeCountTool) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{Type: "object"}
}
func (t invokeCountTool) Call(context.Context, json.RawMessage) (string, error) {
	t.n.Add(1)
	return "ok", nil
}

func TestRunTurnSkipsToolBodiesOnLengthStopReason(t *testing.T) {
	t.Parallel()

	var invocations atomic.Int32
	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{
		{
			StopReason: agentkit.AssistantStopReasonLength,
			ToolCalls: []agentkit.ToolCall{
				{ID: "a", Name: "invoke_count", Input: json.RawMessage(`{}`)},
				{ID: "b", Name: "invoke_count", Input: json.RawMessage(`{}`)},
			},
		},
		{Text: "done"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.Tool{invokeCountTool{n: &invocations}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "test"}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID("test:length-tools")
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(sessionID),
		Workspace:    string(sessionID),
	})
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if invocations.Load() != 0 {
		t.Fatalf("tool bodies ran %d times, want 0", invocations.Load())
	}
}

// Parallel preflight abort: first call passed preflight but body not started; index-aligned cleanup must interrupt it too.
func TestRunTurnInterruptsWhenParallelPreflightAborts(t *testing.T) {
	t.Parallel()

	var invocations atomic.Int32
	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{
		{ToolCalls: []agentkit.ToolCall{
			{ID: "c1", Name: "invoke_count", Input: json.RawMessage(`{}`)},
			{ID: "c2", Name: "invoke_count", Input: json.RawMessage(`{}`)},
			{ID: "c3", Name: "invoke_count", Input: json.RawMessage(`{}`)},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	hook := &abortAfterFirstToolHook{}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.Tool{invokeCountTool{n: &invocations}},
		Hooks: hook,
	})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "test"}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID("test:abort-parallel-preflight")
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(sessionID),
		Workspace:    string(sessionID),
	})
	err = ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
		},
	})
	if !agentkit.IsTurnAbort(err) {
		t.Fatalf("run turn: %v, want turn abort", err)
	}
	if invocations.Load() != 0 {
		t.Fatalf("tool bodies ran %d times, want 0", invocations.Load())
	}

	sess, err := store.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := derive.ReadAllEvents(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []agentkit.ToolCallID{"c1", "c2", "c3"} {
		var found agentkit.ToolResult
		for _, ev := range events {
			if ev.Type != agentkit.EventToolResult {
				continue
			}
			var res agentkit.ToolResult
			if err := json.Unmarshal(ev.Data, &res); err != nil {
				t.Fatal(err)
			}
			if res.ID == id {
				found = res
				break
			}
		}
		if found.Content != derive.InterruptedToolResultText {
			t.Fatalf("%s result = %#v, want interrupted", id, found)
		}
	}
}
