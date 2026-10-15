package compaction

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
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

	if _, err := NewTokenLimit(TokenLimitConfig{}, TokenLimitDeps{Chain: mustChain(t), Services: []capcompaction.Service{&countingService{}}}); err == nil {
		t.Fatal("expected an error without maxTokens or contextWindow")
	}
	if _, err := NewTokenLimit(TokenLimitConfig{MaxTokens: 100}, TokenLimitDeps{Chain: mustChain(t)}); err == nil {
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
	}, TokenLimitDeps{Chain: mustChain(t), Services: []capcompaction.Service{inner}})
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
		Chain:    mustChain(t),
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
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:tokenlimit"})
	if err != nil {
		t.Fatal(err)
	}
	// The provider measured a 9000-token prompt. The character heuristic sees
	// almost nothing, because CJK and dense payloads defeat chars/4 — the
	// measurement has to win.
	if err := sessevents.Default.AppendUsage(ctx, sess, "a", capsession.UsageData{
		InputTokens:  9000,
		OutputTokens: 500,
		TotalTokens:  9500,
	}); err != nil {
		t.Fatal(err)
	}

	inner := &countingService{}
	svc, err := NewTokenLimit(TokenLimitConfig{MaxTokens: 8000}, TokenLimitDeps{
		Chain:    mustChain(t),
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

// fullLogSession emulates the db-backed session contract: after a compaction
// trimmed the in-memory cache, Read(0) serves the FULL durable event log,
// including messages that compaction long since removed from the live context.
type fullLogSession struct {
	id     agentkit.SessionID
	events []agentkit.SessionEvent
}

func (s *fullLogSession) ID() agentkit.SessionID { return s.id }
func (s *fullLogSession) Append(_ context.Context, ev agentkit.SessionEvent) (agentkit.EventSeq, error) {
	ev.Seq = agentkit.EventSeq(len(s.events) + 1)
	s.events = append(s.events, ev)
	return ev.Seq, nil
}
func (s *fullLogSession) Read(_ context.Context, from agentkit.EventSeq) ([]agentkit.SessionEvent, error) {
	out := make([]agentkit.SessionEvent, 0, len(s.events))
	for _, ev := range s.events {
		if ev.Seq > from {
			out = append(out, ev)
		}
	}
	return out, nil
}
func (s *fullLogSession) DeriveMessages(context.Context) ([]agentkit.ModelMessage, error) {
	return nil, nil
}

// Regression: a long session whose durable log holds megabytes of already
// compacted-away history must not trip the gate when the LIVE context is
// small. The pre-fix estimate summed logical chars over Read(0) — cumulative
// across compactions — so every step compacted once the log crossed the
// threshold (observed: 43 compactions in a single 58-step turn).
func TestTokenLimitIgnoresCompactedAwaySessionHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := &fullLogSession{id: "test:full-log"}
	// Two weeks of heavy history, long since compacted away: the durable log
	// keeps it, the live context does not.
	huge := strings.Repeat("h", 2_000_000)
	raw, err := json.Marshal(agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: huge}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Append(ctx, agentkit.SessionEvent{
		Type: agentkit.EventUserMessage, AgentID: "a", Data: raw,
	}); err != nil {
		t.Fatal(err)
	}

	inner := &countingService{}
	svc, err := NewTokenLimit(TokenLimitConfig{MaxTokens: 8000, CharsPerToken: 4}, TokenLimitDeps{
		Chain:    mustChain(t),
		Services: []capcompaction.Service{inner},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Live context is tiny: only the historical log is large.
	if _, err := svc.Compact(ctx, capcompaction.Request{
		SessionID: sess.ID(),
		Session:   sess,
		Messages:  messagesOfSize(400),
	}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 0 {
		t.Fatalf("inner calls = %d, want 0: compacted-away history must not trip the gate", inner.calls)
	}
}

func TestTokenLimitTracksUsageAfterCompaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:posttrim"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendUsage(ctx, sess, "a", capsession.UsageData{InputTokens: 9000, TotalTokens: 9000}); err != nil {
		t.Fatal(err)
	}
	// After compaction the next step reports a much smaller prompt; the gate
	// reads the latest event, so it must close again.
	if err := sessevents.Default.AppendUsage(ctx, sess, "a", capsession.UsageData{InputTokens: 1200, TotalTokens: 1200}); err != nil {
		t.Fatal(err)
	}

	inner := &countingService{}
	svc, err := NewTokenLimit(TokenLimitConfig{MaxTokens: 8000}, TokenLimitDeps{
		Chain:    mustChain(t),
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
