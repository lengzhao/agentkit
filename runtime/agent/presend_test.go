package agent_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/runtime/tools"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// sizingLLM captures the size of each prompt actually sent.
type sizingLLM struct {
	calls     atomic.Int32
	lastChars atomic.Int64
}

func (r *sizingLLM) Name() string { return "recording" }

func (r *sizingLLM) Stream(_ context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	r.calls.Add(1)
	total := 0
	for _, msg := range req.Messages {
		for _, part := range msg.Content {
			total += len(part.Text) + len(part.URL)
		}
	}
	r.lastChars.Store(int64(total))
	return &instantStream{msg: agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "ok"}},
	}}, nil
}

func newPreSendAgent(t *testing.T, mem agentkit.Session, llm agentkit.LLMProvider, svc compaction.Service, maxPromptTokens int) agentkit.Agent {
	t.Helper()
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	rt, err := agent.New(agent.Config{
		ID:              "test",
		Model:           "recording",
		Retry:           &agent.RetryConfig{Enabled: &disabled},
		MaxPromptTokens: maxPromptTokens,
	}, agent.Deps{
		SessionStore: sessstore.NewStaticStore(mem),
		LLM:          llm,
		Tools:        toolRuntime,
		Prompt:       assembler,
		Compaction:   []compaction.Service{svc},
		Workspace:    rtworkspace.Static(t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

// The pre-send guard must compact an oversized assembled prompt (including
// content the before-step estimate cannot see) before the LLM call, so the
// provider never receives the oversized request.
func TestRunTurnPreSendGuardCompacts(t *testing.T) {
	t.Parallel()

	llm := &sizingLLM{}
	compact := &forceCompaction{}
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "presend-s1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(mem.ID()), Workspace: string(mem.ID())})
	if err := sessevents.Default.AppendMessage(ctx, mem, "test", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("x", 100000)}},
	}); err != nil {
		t.Fatal(err)
	}

	rt := newPreSendAgent(t, mem, llm, compact, 1000)
	err = rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	})
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if got := compact.calls.Load(); got != 1 {
		t.Fatalf("forced compaction calls = %d, want 1", got)
	}
	if got := llm.calls.Load(); got != 1 {
		t.Fatalf("llm calls = %d, want 1", got)
	}
	if got := llm.lastChars.Load(); got >= 100000 {
		t.Fatalf("llm received oversized prompt, chars=%d", got)
	}
}

// When compaction cannot shrink the prompt (no real compaction event), the
// guard must fail the step instead of sending a request that will 400.
func TestRunTurnPreSendGuardFailsWhenCompactionDoesNotApply(t *testing.T) {
	t.Parallel()

	llm := &sizingLLM{}
	compact := &fakeAppliedService{}
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "presend-s2"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(mem.ID()), Workspace: string(mem.ID())})
	if err := sessevents.Default.AppendMessage(ctx, mem, "test", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("x", 100000)}},
	}); err != nil {
		t.Fatal(err)
	}

	rt := newPreSendAgent(t, mem, llm, compact, 1000)
	err = rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	})
	if err == nil {
		t.Fatal("expected pre-send guard failure")
	}
	if !strings.Contains(err.Error(), "maxPromptTokens") {
		t.Fatalf("error should mention maxPromptTokens, got: %v", err)
	}
	if got := llm.calls.Load(); got != 0 {
		t.Fatalf("llm calls = %d, want 0: oversized prompt must not be sent", got)
	}
}

// Even a real compaction event may not bring the prompt under the limit (e.g.
// an oversized system prompt that compaction cannot touch). The guard must
// then fail the step instead of sending the request.
func TestRunTurnPreSendGuardFailsWhenStillOversized(t *testing.T) {
	t.Parallel()

	llm := &sizingLLM{}
	compact := &forceCompaction{}
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "presend-s3"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(mem.ID()), Workspace: string(mem.ID())})
	if err := sessevents.Default.AppendMessage(ctx, mem, "test", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("x", 100000)}},
	}); err != nil {
		t.Fatal(err)
	}

	// Limit of 1 token: even the compacted summary + new input exceed it.
	rt := newPreSendAgent(t, mem, llm, compact, 1)
	err = rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	})
	if err == nil {
		t.Fatal("expected pre-send guard failure")
	}
	if !strings.Contains(err.Error(), "still exceeds maxPromptTokens") {
		t.Fatalf("error should report still-oversized prompt, got: %v", err)
	}
	if got := compact.calls.Load(); got != 1 {
		t.Fatalf("forced compaction calls = %d, want 1", got)
	}
	if got := llm.calls.Load(); got != 0 {
		t.Fatalf("llm calls = %d, want 0: oversized prompt must not be sent", got)
	}
}
