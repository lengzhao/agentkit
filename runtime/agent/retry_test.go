package agent_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
)

type flakyLLM struct {
	calls atomic.Int32
}

func (f *flakyLLM) Name() string { return "flaky" }

func (f *flakyLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	call := f.calls.Add(1)
	if call == 1 {
		return nil, fmt.Errorf("rate limit exceeded")
	}
	scripted, _ := newInstantLLM("recovered")
	return scripted.Stream(context.Background(), agentkit.LLMRequest{})
}

type instantLLM struct {
	text string
}

func newInstantLLM(text string) (*instantLLM, error) {
	return &instantLLM{text: text}, nil
}

func (s *instantLLM) Name() string { return "instant" }

func (s *instantLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	return &instantStream{msg: agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: s.text}},
	}}, nil
}

func TestRunTurnRetriesTransientLLMError(t *testing.T) {
	t.Parallel()

	flaky := &flakyLLM{}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := session.NewMemory(session.MemoryConfig{ID: "retry-s1"})
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	rt, err := agent.New(agent.Config{
		ID:    "test",
		Model: "flaky",
		Retry: &agent.RetryConfig{Enabled: &enabled, MaxRetries: 3, BaseDelayMs: 1},
	}, agent.Deps{
		SessionStore: session.NewStaticStore(mem),
		LLM:          flaky,
		Tools:        toolRuntime,
		Prompt:       assembler,
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
	if flaky.calls.Load() != 2 {
		t.Fatalf("expected 2 llm calls, got %d", flaky.calls.Load())
	}

	events, err := session.ReadAllEvents(ctx, mem)
	if err != nil {
		t.Fatal(err)
	}
	var retryStarts, retryEnds int
	for _, ev := range events {
		switch ev.Type {
		case agentkit.EventAutoRetryStart:
			retryStarts++
		case agentkit.EventAutoRetryEnd:
			retryEnds++
		}
	}
	if retryStarts != 1 || retryEnds != 1 {
		t.Fatalf("retry events start=%d end=%d", retryStarts, retryEnds)
	}
}

func TestRunTurnDoesNotRetryQuotaError(t *testing.T) {
	t.Parallel()

	quota := &quotaLLM{}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := session.NewMemory(session.MemoryConfig{ID: "quota-s1"})
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	rt, err := agent.New(agent.Config{
		ID:    "test",
		Model: "quota",
		Retry: &agent.RetryConfig{Enabled: &enabled, MaxRetries: 3, BaseDelayMs: 1},
	}, agent.Deps{
		SessionStore: session.NewStaticStore(mem),
		LLM:          quota,
		Tools:        toolRuntime,
		Prompt:       assembler,
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
		t.Fatal("expected quota error")
	}
	if quota.calls.Load() != 1 {
		t.Fatalf("expected 1 llm call, got %d", quota.calls.Load())
	}
}

type quotaLLM struct {
	calls atomic.Int32
}

func (q *quotaLLM) Name() string { return "quota" }

func (q *quotaLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	q.calls.Add(1)
	return nil, fmt.Errorf("insufficient_quota")
}
