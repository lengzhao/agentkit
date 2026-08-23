package agent_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
)

type overflowLLM struct {
	calls atomic.Int32
}

func (o *overflowLLM) Name() string { return "overflow" }

func (o *overflowLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	if o.calls.Add(1) == 1 {
		return nil, fmt.Errorf("maximum context length exceeded")
	}
	return &instantStream{msg: agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "recovered"}},
	}}, nil
}

type forceCompaction struct {
	calls atomic.Int32
}

func (f *forceCompaction) Compact(ctx context.Context, req compaction.Request) (compaction.Result, error) {
	if !req.Force {
		return compaction.Result{}, nil
	}
	f.calls.Add(1)
	if req.Session == nil {
		return compaction.Result{}, fmt.Errorf("session required")
	}
	events, err := session.ReadAllEvents(ctx, req.Session)
	if err != nil {
		return compaction.Result{}, err
	}
	if err := session.AppendCompaction(ctx, req.Session, req.AgentID, compaction.EventData{
		BeforeSeq: session.LatestEventSeq(events),
		Kind:      compaction.KindSummary,
		Summary: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "[Conversation summary]\nshort"}},
		},
	}); err != nil {
		return compaction.Result{}, err
	}
	return compaction.Result{Applied: true}, nil
}

func TestRunTurnOverflowCompactAndRetry(t *testing.T) {
	t.Parallel()

	llm := &overflowLLM{}
	compact := &forceCompaction{}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := session.NewMemory(session.MemoryConfig{ID: "overflow-s1"})
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	rt, err := agent.New(agent.Config{
		ID:    "test",
		Model: "overflow",
		Retry: &agent.RetryConfig{Enabled: &disabled},
	}, agent.Deps{
		SessionStore: session.NewStaticStore(mem),
		LLM:          llm,
		Tools:        toolRuntime,
		Prompt:       assembler,
		Compaction:   []compaction.Service{compact},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, mem.ID())
	err = rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	})
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if llm.calls.Load() != 2 {
		t.Fatalf("expected 2 llm calls, got %d", llm.calls.Load())
	}
	if compact.calls.Load() != 1 {
		t.Fatalf("expected 1 forced compaction, got %d", compact.calls.Load())
	}

	events, err := session.ReadAllEvents(ctx, mem)
	if err != nil {
		t.Fatal(err)
	}
	var overflowEvents int
	for _, ev := range events {
		if ev.Type == agentkit.EventOverflowRecovery {
			overflowEvents++
		}
	}
	if overflowEvents != 1 {
		t.Fatalf("overflow recovery events=%d", overflowEvents)
	}
}

func TestRunTurnOverflowRecoveryOnlyOnce(t *testing.T) {
	t.Parallel()

	llm := &alwaysOverflowLLM{}
	compact := &forceCompaction{}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := session.NewMemory(session.MemoryConfig{ID: "overflow-s2"})
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	rt, err := agent.New(agent.Config{
		ID:    "test",
		Model: "overflow",
		Retry: &agent.RetryConfig{Enabled: &disabled},
	}, agent.Deps{
		SessionStore: session.NewStaticStore(mem),
		LLM:          llm,
		Tools:        toolRuntime,
		Prompt:       assembler,
		Compaction:   []compaction.Service{compact},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, mem.ID())
	err = rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	})
	if err == nil {
		t.Fatal("expected overflow failure")
	}
	if compact.calls.Load() != 1 {
		t.Fatalf("expected single compaction attempt, got %d", compact.calls.Load())
	}
}

type alwaysOverflowLLM struct{}

func (alwaysOverflowLLM) Name() string { return "always-overflow" }

func (alwaysOverflowLLM) Stream(context.Context, agentkit.LLMRequest) (agentkit.LLMStream, error) {
	return nil, fmt.Errorf("maximum context length exceeded")
}
