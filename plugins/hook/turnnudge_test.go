package hook

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func newNudgeProvider(t *testing.T, cfg TurnNudgeConfig) (*turnNudgeProvider, *fakeClock) {
	t.Helper()
	raw, err := NewTurnNudge(cfg, TurnNudgeDeps{})
	if err != nil {
		t.Fatalf("build hook/turn-nudge: %v", err)
	}
	p, ok := raw.(*turnNudgeProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *turnNudgeProvider", raw)
	}
	clock := &fakeClock{now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
	p.now = clock.Now
	return p, clock
}

func nudgeCtx(sessionID string) context.Context {
	return rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: sessionID,
		Workspace:    sessionID,
	})
}

func startTurn(t *testing.T, p *turnNudgeProvider, ctx context.Context) {
	t.Helper()
	if err := p.beforeStep(ctx, &agentkit.BeforeStep{Step: 0, Segment: 0}); err != nil {
		t.Fatalf("beforeStep: %v", err)
	}
}

func callTool(t *testing.T, p *turnNudgeProvider, ctx context.Context, content string) agentkit.ToolResult {
	t.Helper()
	result := agentkit.ToolResult{ID: "c1", Name: "demo", Content: content}
	if err := p.afterTool(ctx, &result); err != nil {
		t.Fatalf("afterTool: %v", err)
	}
	return result
}

func TestTurnNudgeConfigDefaultsAndValidate(t *testing.T) {
	t.Parallel()

	cfg := TurnNudgeConfig{}
	cfg.SetDefaults()
	if cfg.ToolCallInterval != defaultToolCallInterval {
		t.Fatalf("ToolCallInterval = %d, want %d", cfg.ToolCallInterval, defaultToolCallInterval)
	}
	if cfg.TimeIntervalMinutes != defaultTimeIntervalMinutes {
		t.Fatalf("TimeIntervalMinutes = %d, want %d", cfg.TimeIntervalMinutes, defaultTimeIntervalMinutes)
	}
	if cfg.Message != defaultNudgeMessage {
		t.Fatalf("Message = %q, want default", cfg.Message)
	}
	if cfg.ToolCallLimit != defaultToolCallLimit {
		t.Fatalf("ToolCallLimit = %d, want %d", cfg.ToolCallLimit, defaultToolCallLimit)
	}
	if cfg.TimeLimitMinutes != defaultTimeLimitMinutes {
		t.Fatalf("TimeLimitMinutes = %d, want %d", cfg.TimeLimitMinutes, defaultTimeLimitMinutes)
	}
	if cfg.LimitMessage != defaultLimitMessage {
		t.Fatalf("LimitMessage = %q, want default", cfg.LimitMessage)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate defaults: %v", err)
	}
	bad := TurnNudgeConfig{ToolCallInterval: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("negative toolCallInterval should fail validation")
	}
	bad = TurnNudgeConfig{TimeIntervalMinutes: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("negative timeIntervalMinutes should fail validation")
	}
	bad = TurnNudgeConfig{ToolCallLimit: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("negative toolCallLimit should fail validation")
	}
	bad = TurnNudgeConfig{TimeLimitMinutes: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("negative timeLimitMinutes should fail validation")
	}
}

func TestNudgeAtToolCallInterval(t *testing.T) {
	t.Parallel()

	p, _ := newNudgeProvider(t, TurnNudgeConfig{ToolCallInterval: 3, TimeIntervalMinutes: 100000})
	ctx := nudgeCtx("sess-count")
	startTurn(t, p, ctx)

	for i := 1; i <= 2; i++ {
		if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
			t.Fatalf("call %d content = %q, want unchanged", i, got)
		}
	}
	got := callTool(t, p, ctx, "ok").Content
	if !strings.Contains(got, "ok\n\n") || !strings.Contains(got, "3 tool calls") {
		t.Fatalf("3rd call should carry notice, content = %q", got)
	}
	// Next nudge at 6 calls.
	for i := 4; i <= 5; i++ {
		if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
			t.Fatalf("call %d content = %q, want unchanged", i, got)
		}
	}
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "6 tool calls") {
		t.Fatalf("6th call should carry notice, content = %q", got)
	}
}

func TestNudgeAtTimeInterval(t *testing.T) {
	t.Parallel()

	p, clock := newNudgeProvider(t, TurnNudgeConfig{ToolCallInterval: 100000, TimeIntervalMinutes: 20})
	ctx := nudgeCtx("sess-time")
	startTurn(t, p, ctx)

	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("early call content = %q, want unchanged", got)
	}
	clock.Advance(21 * time.Minute)
	got := callTool(t, p, ctx, "ok").Content
	if !strings.Contains(got, "2 tool calls") || !strings.Contains(got, "21m0s") {
		t.Fatalf("time-based notice missing, content = %q", got)
	}
	// Next time nudge only after another full interval.
	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("call right after notice content = %q, want unchanged", got)
	}
	clock.Advance(20 * time.Minute)
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "4 tool calls") || !strings.Contains(got, "41m0s") {
		t.Fatalf("second time-based notice missing, content = %q", got)
	}
}

func TestLimitMessageAfterToolCallLimit(t *testing.T) {
	t.Parallel()

	p, _ := newNudgeProvider(t, TurnNudgeConfig{
		ToolCallInterval:    2,
		TimeIntervalMinutes: 100000,
		ToolCallLimit:       3,
		TimeLimitMinutes:    100000,
	})
	ctx := nudgeCtx("sess-limit-count")
	startTurn(t, p, ctx)

	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("call 1 content = %q, want unchanged", got)
	}
	// Pre-limit: periodic status only, no wrap-up directive.
	got := callTool(t, p, ctx, "ok").Content
	if !strings.Contains(got, "Budget status") || strings.Contains(got, "Summarize") {
		t.Fatalf("call 2 should carry status-only notice, content = %q", got)
	}
	// At and beyond the limit: every result carries the wrap-up-and-end message.
	for i := 3; i <= 5; i++ {
		got := callTool(t, p, ctx, "ok").Content
		if !strings.Contains(got, "LIMIT REACHED") || !strings.Contains(got, "Summarize") {
			t.Fatalf("call %d should carry limit message, content = %q", i, got)
		}
	}
}

func TestLimitMessageAfterTimeLimit(t *testing.T) {
	t.Parallel()

	p, clock := newNudgeProvider(t, TurnNudgeConfig{
		ToolCallInterval:    100000,
		TimeIntervalMinutes: 100000,
		ToolCallLimit:       100000,
		TimeLimitMinutes:    30,
	})
	ctx := nudgeCtx("sess-limit-time")
	startTurn(t, p, ctx)

	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("early call content = %q, want unchanged", got)
	}
	clock.Advance(31 * time.Minute)
	got := callTool(t, p, ctx, "ok").Content
	if !strings.Contains(got, "LIMIT REACHED") || !strings.Contains(got, "31m0s") {
		t.Fatalf("time limit should trigger limit message, content = %q", got)
	}
	// Every subsequent result keeps carrying it.
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "LIMIT REACHED") {
		t.Fatalf("limit message should repeat, content = %q", got)
	}
}

func TestNudgeEmptyResultContent(t *testing.T) {
	t.Parallel()

	p, _ := newNudgeProvider(t, TurnNudgeConfig{ToolCallInterval: 1, TimeIntervalMinutes: 100000})
	ctx := nudgeCtx("sess-empty")
	startTurn(t, p, ctx)

	got := callTool(t, p, ctx, "").Content
	if strings.HasPrefix(got, "\n") {
		t.Fatalf("empty content should not gain leading separator, content = %q", got)
	}
	if !strings.Contains(got, "1 tool calls") {
		t.Fatalf("notice missing, content = %q", got)
	}
}

func TestNudgeResetsOnNewTurn(t *testing.T) {
	t.Parallel()

	p, clock := newNudgeProvider(t, TurnNudgeConfig{ToolCallInterval: 2, TimeIntervalMinutes: 20})
	ctx := nudgeCtx("sess-reset")
	startTurn(t, p, ctx)
	callTool(t, p, ctx, "ok")
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "2 tool calls") {
		t.Fatalf("2nd call should carry notice, content = %q", got)
	}

	// New turn: counters and clock baseline reset.
	clock.Advance(time.Hour)
	startTurn(t, p, ctx)
	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("first call of new turn content = %q, want unchanged", got)
	}
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "2 tool calls") {
		t.Fatalf("2nd call of new turn should carry notice, content = %q", got)
	}
}

func TestNudgeWithoutSessionOrTurnStart(t *testing.T) {
	t.Parallel()

	p, _ := newNudgeProvider(t, TurnNudgeConfig{ToolCallInterval: 2, TimeIntervalMinutes: 20})

	// No session in ctx: no-op.
	result := agentkit.ToolResult{Content: "ok"}
	if err := p.afterTool(context.Background(), &result); err != nil {
		t.Fatalf("afterTool without session: %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("content = %q, want unchanged", result.Content)
	}

	// No beforeStep (defensive lazy init): still counts and nudges.
	ctx := nudgeCtx("sess-lazy")
	callTool(t, p, ctx, "ok")
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "2 tool calls") {
		t.Fatalf("lazy state should still nudge, content = %q", got)
	}
}

func TestNudgeCustomMessage(t *testing.T) {
	t.Parallel()

	p, _ := newNudgeProvider(t, TurnNudgeConfig{
		ToolCallInterval:    1,
		TimeIntervalMinutes: 100000,
		Message:             "WRAP UP calls={toolCalls} elapsed={elapsed}",
	})
	ctx := nudgeCtx("sess-custom")
	startTurn(t, p, ctx)

	got := callTool(t, p, ctx, "ok").Content
	if !strings.Contains(got, "WRAP UP calls=1 elapsed=0s") {
		t.Fatalf("custom message not applied, content = %q", got)
	}
}
