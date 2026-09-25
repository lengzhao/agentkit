package agent_test

import (
	"context"
	"encoding/json"
	"errors"
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

type failCallTool struct{}

func (failCallTool) Name() string        { return "fail_call" }
func (failCallTool) Description() string { return "always returns a recoverable execute error" }
func (failCallTool) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{Type: "object"}
}
func (failCallTool) Call(context.Context, json.RawMessage) (string, error) {
	return "", errors.New("invalid tool input: bad field")
}

type slowTool struct{}

func (slowTool) Name() string        { return "slow" }
func (slowTool) Description() string { return "slow tool" }
func (slowTool) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{Type: "object"}
}
func (slowTool) Call(ctx context.Context, _ json.RawMessage) (string, error) {
	select {
	case <-time.After(2 * time.Second):
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestRunTurnContinuesAfterToolTimeout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{
		{ToolCalls: []agentkit.ToolCall{{ID: "slow-1", Name: "slow", Input: json.RawMessage(`{}`)}}},
		{Text: "after timeout"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{
		ToolTimeouts: map[string]int{"slow": 1},
	}, tools.RuntimeDeps{Tools: []agentkit.Tool{slowTool{}}})
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
	sessionID := agentkit.SessionID("test:timeout")
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
	events, err := derive.ReadAllEvents(context.Background(), mustSession(t, store, sessionID))
	if err != nil {
		t.Fatal(err)
	}
	var sawTimeout bool
	for _, ev := range events {
		if ev.Type != agentkit.EventToolResult {
			continue
		}
		var res agentkit.ToolResult
		if err := json.Unmarshal(ev.Data, &res); err != nil {
			t.Fatal(err)
		}
		if res.ID == "slow-1" && res.Audit["decision"] == "timeout" {
			sawTimeout = true
		}
	}
	if !sawTimeout {
		t.Fatal("expected tool timeout result in session")
	}
}

func mustSession(t *testing.T, store agentkit.SessionStore, id agentkit.SessionID) agentkit.Session {
	t.Helper()
	sess, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestRunTurnRecoversFromToolExecuteError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{
		{ToolCalls: []agentkit.ToolCall{{
			ID: "bad-1", Name: "fail_call", Input: json.RawMessage(`{}`),
		}}},
		{Text: "recovered"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.Tool{failCallTool{}},
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
	sessionID := agentkit.SessionID("test:recover")
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

	sess, err := store.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := derive.ReadAllEvents(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	var sawErrorResult bool
	for _, ev := range events {
		if ev.Type != agentkit.EventToolResult {
			continue
		}
		var res agentkit.ToolResult
		if err := json.Unmarshal(ev.Data, &res); err != nil {
			t.Fatal(err)
		}
		if res.ID == "bad-1" && res.Audit["decision"] == "error" {
			sawErrorResult = true
		}
	}
	if !sawErrorResult {
		t.Fatal("expected recoverable tool error persisted as tool/result")
	}
}

type abortAfterFirstToolHook struct {
	seen int
}

func (h *abortAfterFirstToolHook) BeforeStep(context.Context, *agentkit.BeforeStep) error { return nil }
func (h *abortAfterFirstToolHook) BeforeTool(context.Context, *agentkit.ToolCall) error {
	h.seen++
	if h.seen > 1 {
		return agentkit.AbortTurn(errors.New("hook stop"))
	}
	return nil
}
func (h *abortAfterFirstToolHook) AfterTool(context.Context, *agentkit.ToolResult) error { return nil }
func (h *abortAfterFirstToolHook) TurnStopping(context.Context, *agentkit.TurnStopping) error {
	return nil
}
func (h *abortAfterFirstToolHook) TurnComplete(context.Context, *agentkit.TurnComplete) error { return nil }

func TestRunTurnInterruptedResultsOnAbortMidBatch(t *testing.T) {
	t.Parallel()

	steps := []llm.ScriptedStep{{
		ToolCalls: []agentkit.ToolCall{
			{ID: "c1", Name: "fail_call", Input: json.RawMessage(`{}`)},
			{ID: "c2", Name: "fail_call", Input: json.RawMessage(`{}`)},
		},
	}}
	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	hook := &abortAfterFirstToolHook{}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.Tool{failCallTool{}},
		Hooks: hook,
	})
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
	sessionID := agentkit.SessionID("test:abort-batch")
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

	sess, err := store.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := derive.ReadAllEvents(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[agentkit.ToolCallID]agentkit.ToolResult{}
	for _, ev := range events {
		if ev.Type != agentkit.EventToolResult {
			continue
		}
		var res agentkit.ToolResult
		if err := json.Unmarshal(ev.Data, &res); err != nil {
			t.Fatal(err)
		}
		byID[res.ID] = res
	}
	if byID["c1"].Audit["decision"] != "error" {
		t.Fatalf("c1 result = %#v, want recoverable error", byID["c1"])
	}
	if byID["c2"].Content != derive.InterruptedToolResultText {
		t.Fatalf("c2 result = %#v, want interrupted", byID["c2"])
	}
}
