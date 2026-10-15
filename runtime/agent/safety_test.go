package agent_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/runtime/tools"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// safetyOnceLLM fails the first call with a provider content-safety rejection,
// then serves a normal completion after the compact-and-retry recovery.
type safetyOnceLLM struct {
	calls atomic.Int32
}

func (o *safetyOnceLLM) Name() string { return "safety-once" }

func (o *safetyOnceLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	if o.calls.Add(1) == 1 {
		return nil, fmt.Errorf("error, status code: 400, message: The input text was flagged by safety checks")
	}
	return &instantStream{msg: agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "recovered"}},
	}}, nil
}

// alwaysSafetyLLM keeps rejecting: the turn must end gracefully, not hard-fail.
type alwaysSafetyLLM struct {
	calls atomic.Int32
}

func (o *alwaysSafetyLLM) Name() string { return "safety-always" }

func (o *alwaysSafetyLLM) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	o.calls.Add(1)
	return nil, fmt.Errorf("error, status code: 400, message: The input text was flagged by safety checks")
}

func newSafetyRuntime(t *testing.T, llm agentkit.LLMProvider, mem agentkit.Session, compact *forceCompaction) agentkit.Agent {
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
		ID:    "test",
		Model: "safety",
		Retry: &agent.RetryConfig{Enabled: &disabled},
	}, agent.Deps{
		SessionStore: sessstore.NewStaticStore(mem),
		LLM:          llm,
		Tools:        toolRuntime,
		Prompt:       assembler,
		Compaction:   []compaction.Service{compact},
		Workspace:    rtworkspace.Static(t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func runSafetyTurn(t *testing.T, rt agentkit.Agent, mem agentkit.Session) error {
	t.Helper()
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(mem.ID()), Workspace: string(mem.ID())})
	return rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	})
}

func safetyRecoveryEvents(t *testing.T, ctx context.Context, mem agentkit.Session) int {
	t.Helper()
	events, err := derive.ReadAllEvents(ctx, mem)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, ev := range events {
		if ev.Type == agentkit.EventOverflowRecovery && strings.Contains(string(ev.Data), "safety") {
			count++
		}
	}
	return count
}

// 安全拦截后：强制 compaction 替换历史 → 重试成功 → 轮次正常完成。
func TestRunTurnSafetyCompactAndRetry(t *testing.T) {
	t.Parallel()

	llm := &safetyOnceLLM{}
	compact := &forceCompaction{}
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "safety-s1"})
	if err != nil {
		t.Fatal(err)
	}
	rt := newSafetyRuntime(t, llm, mem, compact)

	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(mem.ID()), Workspace: string(mem.ID())})
	if err := rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if llm.calls.Load() != 2 {
		t.Fatalf("expected 2 llm calls, got %d", llm.calls.Load())
	}
	if compact.calls.Load() != 1 {
		t.Fatalf("expected 1 forced compaction, got %d", compact.calls.Load())
	}
	if got := safetyRecoveryEvents(t, ctx, mem); got != 1 {
		t.Fatalf("safety recovery events = %d, want 1", got)
	}
}

// 安全拦截持续发生：compaction 重试一次后仍被拒绝 → 合成解释消息优雅收尾，
// 轮次不得硬失败（事故回归：57 步后 turn 以 NO OUTPUT 死亡）。
func TestRunTurnSafetyPersistsEndsGracefully(t *testing.T) {
	t.Parallel()

	llm := &alwaysSafetyLLM{}
	compact := &forceCompaction{}
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "safety-s2"})
	if err != nil {
		t.Fatal(err)
	}
	rt := newSafetyRuntime(t, llm, mem, compact)

	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(mem.ID()), Workspace: string(mem.ID())})
	if err := rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	}); err != nil {
		t.Fatalf("safety-blocked turn must not hard-fail, got %v", err)
	}
	// 初次 + compaction 后重试一次，不得无限重试。
	if llm.calls.Load() != 2 {
		t.Fatalf("expected 2 llm calls (initial + one compact-retry), got %d", llm.calls.Load())
	}
	if compact.calls.Load() != 1 {
		t.Fatalf("expected 1 forced compaction, got %d", compact.calls.Load())
	}

	messages, err := mem.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	last := messages[len(messages)-1]
	if last.Role != "assistant" {
		t.Fatalf("last message role = %q, want assistant", last.Role)
	}
	if len(last.Content) == 0 || !strings.Contains(last.Content[0].Text, "安全") {
		t.Fatalf("final message must explain the safety block, got %#v", last.Content)
	}
	if got := safetyRecoveryEvents(t, ctx, mem); got != 1 {
		t.Fatalf("safety recovery events = %d, want 1", got)
	}

	// 必须写入 run/finish(blocked)：否则自主运行的 turn-continue hook 只看
	// pending todos，会把确定性拒绝的安全错误当成"还有活"反复续跑，每段都
	// 向用户重投同一条拦截通知。
	events, err := derive.ReadAllEvents(ctx, mem)
	if err != nil {
		t.Fatal(err)
	}
	finish := capsession.FinishAfter(events, capsession.RunStartSeq(events))
	if finish == nil || finish.Status != capsession.FinishBlocked {
		t.Fatalf("safety-blocked turn must record run/finish(blocked), got %+v", finish)
	}
}
