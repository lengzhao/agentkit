package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/session"
)

// countingService stands in for the gated compaction chain so tests can assert
// whether the gate opened, without running a real summarization.
type countingService struct {
	calls  int
	forced int
}

func (s *countingService) Compact(_ context.Context, req capcompaction.Request) (capcompaction.Result, error) {
	s.calls++
	if req.Force {
		s.forced++
	}
	return capcompaction.Result{Applied: true, Messages: req.Messages}, nil
}

func messagesOfSize(chars int) []agentkit.ModelMessage {
	return []agentkit.ModelMessage{{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("x", chars)}},
	}}
}

func TestTokenLimitRequiresAThreshold(t *testing.T) {
	t.Parallel()

	if _, err := NewTokenLimit(TokenLimitConfig{}, TokenLimitDeps{Services: []capcompaction.Service{&countingService{}}}); err == nil {
		t.Fatal("expected an error without maxTokens or contextWindow")
	}
	if _, err := NewTokenLimit(TokenLimitConfig{MaxTokens: 100}, TokenLimitDeps{}); err == nil {
		t.Fatal("expected an error without a service to gate")
	}
}

func TestTokenLimitDerivesThresholdFromContextWindow(t *testing.T) {
	t.Parallel()

	inner := &countingService{}
	svc, err := NewTokenLimit(TokenLimitConfig{
		ContextWindow: 1000,
		TriggerRatio:  0.5,
		CharsPerToken: 4,
	}, TokenLimitDeps{Services: []capcompaction.Service{inner}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// 1600 chars / 4 = 400 tokens, below the 500-token trigger.
	if _, err := svc.Compact(ctx, capcompaction.Request{Messages: messagesOfSize(1600)}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 0 {
		t.Fatalf("inner calls = %d, want 0 below the threshold", inner.calls)
	}

	// 2400 chars / 4 = 600 tokens, above it.
	if _, err := svc.Compact(ctx, capcompaction.Request{Messages: messagesOfSize(2400)}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1 above the threshold", inner.calls)
	}
	if inner.forced != 1 {
		t.Fatalf("inner forced = %d, want 1: crossing the threshold IS the gate", inner.forced)
	}
}

func TestTokenLimitPassesForcedRequestsThrough(t *testing.T) {
	t.Parallel()

	inner := &countingService{}
	svc, err := NewTokenLimit(TokenLimitConfig{MaxTokens: 1000000}, TokenLimitDeps{
		Services: []capcompaction.Service{inner},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A manual /compact or overflow recovery must not be blocked by the gate.
	if _, err := svc.Compact(context.Background(), capcompaction.Request{
		Messages: messagesOfSize(10),
		Force:    true,
	}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1 for a forced request", inner.calls)
	}
}

func TestTokenLimitUsesReportedUsageWhenLarger(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess, err := session.NewMemory(session.MemoryConfig{ID: "test:tokenlimit"})
	if err != nil {
		t.Fatal(err)
	}
	// The provider measured a 9000-token prompt. The character heuristic sees
	// almost nothing, because CJK and dense payloads defeat chars/4 — the
	// measurement has to win.
	if err := session.AppendUsage(ctx, sess, "a", session.UsageData{
		InputTokens:  9000,
		OutputTokens: 500,
		TotalTokens:  9500,
	}); err != nil {
		t.Fatal(err)
	}

	inner := &countingService{}
	svc, err := NewTokenLimit(TokenLimitConfig{MaxTokens: 8000}, TokenLimitDeps{
		Services: []capcompaction.Service{inner},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(),
		Session:   sess,
		Messages:  messagesOfSize(40),
	}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner calls = %d, want 1: reported usage should trip the gate", inner.calls)
	}
}

func TestTokenLimitTracksUsageAfterCompaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess, err := session.NewMemory(session.MemoryConfig{ID: "test:posttrim"})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendUsage(ctx, sess, "a", session.UsageData{InputTokens: 9000, TotalTokens: 9000}); err != nil {
		t.Fatal(err)
	}
	// After compaction the next step reports a much smaller prompt; the gate
	// reads the latest event, so it must close again.
	if err := session.AppendUsage(ctx, sess, "a", session.UsageData{InputTokens: 1200, TotalTokens: 1200}); err != nil {
		t.Fatal(err)
	}

	inner := &countingService{}
	svc, err := NewTokenLimit(TokenLimitConfig{MaxTokens: 8000}, TokenLimitDeps{
		Services: []capcompaction.Service{inner},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(),
		Session:   sess,
		Messages:  messagesOfSize(40),
	}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 0 {
		t.Fatalf("inner calls = %d, want 0 after the context shrank", inner.calls)
	}
}

func TestTokenLimitGatesRealSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess, err := session.NewMemory(session.MemoryConfig{ID: "test:gatesummary"})
	if err != nil {
		t.Fatal(err)
	}
	// Only two messages: summary's own minMessages gate (default 20) would never
	// fire on its own, which is exactly why the token gate forces it.
	for i := 0; i < 2; i++ {
		if err := session.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("y", 40000)}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	summary, err := NewSummary(SummaryConfig{KeepRecent: 1}, SummaryDeps{LLM: &flakySummaryLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewTokenLimit(TokenLimitConfig{MaxTokens: 10000}, TokenLimitDeps{
		Services: []capcompaction.Service{summary},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(),
		Session:   sess,
		Messages:  messages,
	})
	if err != nil {
		t.Fatalf("gated summary: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected the gated summary to apply")
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	compactions := 0
	for _, ev := range events {
		if ev.Type == agentkit.EventCompaction {
			compactions++
		}
	}
	if compactions != 1 {
		t.Fatalf("session/compaction events = %d, want 1", compactions)
	}
}
