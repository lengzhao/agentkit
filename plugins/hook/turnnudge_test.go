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
	advanceToStep(t, p, ctx, 0)
}

func advanceToStep(t *testing.T, p *turnNudgeProvider, ctx context.Context, step int) {
	t.Helper()
	if err := p.beforeStep(ctx, &agentkit.BeforeStep{Step: step, Segment: 0}); err != nil {
		t.Fatalf("beforeStep(%d): %v", step, err)
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

// bigSteps keeps the step-based tier out of the way for time-focused tests.
const bigSteps = 100000

func TestTurnNudgeConfigDefaultsAndValidate(t *testing.T) {
	t.Parallel()

	cfg := TurnNudgeConfig{}
	cfg.SetDefaults()
	if cfg.StepInterval != defaultStepInterval {
		t.Fatalf("StepInterval = %d, want %d", cfg.StepInterval, defaultStepInterval)
	}
	if cfg.TimeIntervalMinutes != defaultTimeIntervalMinutes {
		t.Fatalf("TimeIntervalMinutes = %d, want %d", cfg.TimeIntervalMinutes, defaultTimeIntervalMinutes)
	}
	if cfg.StepLimit != defaultStepLimit {
		t.Fatalf("StepLimit = %d, want %d", cfg.StepLimit, defaultStepLimit)
	}
	if cfg.TimeLimitMinutes != defaultTimeLimitMinutes {
		t.Fatalf("TimeLimitMinutes = %d, want %d", cfg.TimeLimitMinutes, defaultTimeLimitMinutes)
	}
	if cfg.Message != defaultNudgeMessage {
		t.Fatalf("Message = %q, want default", cfg.Message)
	}
	if cfg.LimitMessage != defaultLimitMessage {
		t.Fatalf("LimitMessage = %q, want default", cfg.LimitMessage)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate defaults: %v", err)
	}
	bad := TurnNudgeConfig{StepInterval: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("negative stepInterval should fail validation")
	}
	bad = TurnNudgeConfig{TimeIntervalMinutes: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("negative timeIntervalMinutes should fail validation")
	}
	bad = TurnNudgeConfig{StepLimit: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("negative stepLimit should fail validation")
	}
	bad = TurnNudgeConfig{TimeLimitMinutes: -1}
	if err := bad.Validate(); err == nil {
		t.Fatal("negative timeLimitMinutes should fail validation")
	}
}

func TestNudgeAtStepInterval(t *testing.T) {
	t.Parallel()

	p, _ := newNudgeProvider(t, TurnNudgeConfig{
		StepInterval:        3,
		TimeIntervalMinutes: bigSteps,
		StepLimit:           bigSteps,
		TimeLimitMinutes:    bigSteps,
	})
	ctx := nudgeCtx("sess-steps")
	startTurn(t, p, ctx)

	for _, s := range []int{1, 2} {
		advanceToStep(t, p, ctx, s)
		if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
			t.Fatalf("step %d content = %q, want unchanged", s, got)
		}
	}
	advanceToStep(t, p, ctx, 3)
	got := callTool(t, p, ctx, "ok").Content
	if !strings.Contains(got, "ok\n\n") || !strings.Contains(got, "3 steps") {
		t.Fatalf("step 3 should carry notice, content = %q", got)
	}
	// A parallel batch in the same step injects only once.
	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("second tool of step 3 content = %q, want unchanged (one notice per step)", got)
	}
	// Next nudge at step 6.
	for _, s := range []int{4, 5} {
		advanceToStep(t, p, ctx, s)
		if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
			t.Fatalf("step %d content = %q, want unchanged", s, got)
		}
	}
	advanceToStep(t, p, ctx, 6)
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "6 steps") {
		t.Fatalf("step 6 should carry notice, content = %q", got)
	}
}

func TestNudgeAtTimeInterval(t *testing.T) {
	t.Parallel()

	p, clock := newNudgeProvider(t, TurnNudgeConfig{
		StepInterval:        bigSteps,
		TimeIntervalMinutes: 10,
		StepLimit:           bigSteps,
		TimeLimitMinutes:    bigSteps,
	})
	ctx := nudgeCtx("sess-time")
	startTurn(t, p, ctx)

	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("early call content = %q, want unchanged", got)
	}
	clock.Advance(11 * time.Minute)
	got := callTool(t, p, ctx, "ok").Content
	if !strings.Contains(got, "Budget status") || !strings.Contains(got, "11m0s") {
		t.Fatalf("time-based notice missing, content = %q", got)
	}
	// Next time nudge only after another full interval.
	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("call right after notice content = %q, want unchanged", got)
	}
	clock.Advance(10 * time.Minute)
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "21m0s") {
		t.Fatalf("second time-based notice missing, content = %q", got)
	}
}

func TestLimitMessageAfterStepLimit(t *testing.T) {
	t.Parallel()

	p, _ := newNudgeProvider(t, TurnNudgeConfig{
		StepInterval:        2,
		TimeIntervalMinutes: bigSteps,
		StepLimit:           3,
		TimeLimitMinutes:    bigSteps,
	})
	ctx := nudgeCtx("sess-limit-steps")
	startTurn(t, p, ctx)

	advanceToStep(t, p, ctx, 1)
	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("step 1 content = %q, want unchanged", got)
	}
	// Pre-limit: periodic status only, no wrap-up directive.
	advanceToStep(t, p, ctx, 2)
	got := callTool(t, p, ctx, "ok").Content
	if !strings.Contains(got, "Budget status") || strings.Contains(got, "Summarize") {
		t.Fatalf("step 2 should carry status-only notice, content = %q", got)
	}
	// At and beyond the limit: every result carries the wrap-up-and-end message.
	for _, s := range []int{3, 4, 5} {
		advanceToStep(t, p, ctx, s)
		got := callTool(t, p, ctx, "ok").Content
		if !strings.Contains(got, "LIMIT REACHED") || !strings.Contains(got, "Summarize") {
			t.Fatalf("step %d should carry limit message, content = %q", s, got)
		}
	}
	// Every tool result within a limited step carries it too.
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "LIMIT REACHED") {
		t.Fatalf("limit message should repeat on every tool result, content = %q", got)
	}
}

func TestLimitMessageAfterTimeLimit(t *testing.T) {
	t.Parallel()

	p, clock := newNudgeProvider(t, TurnNudgeConfig{
		StepInterval:        bigSteps,
		TimeIntervalMinutes: bigSteps,
		StepLimit:           bigSteps,
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

	p, _ := newNudgeProvider(t, TurnNudgeConfig{
		StepInterval:        1,
		TimeIntervalMinutes: bigSteps,
		StepLimit:           bigSteps,
		TimeLimitMinutes:    bigSteps,
	})
	ctx := nudgeCtx("sess-empty")
	startTurn(t, p, ctx)

	advanceToStep(t, p, ctx, 1)
	got := callTool(t, p, ctx, "").Content
	if strings.HasPrefix(got, "\n") {
		t.Fatalf("empty content should not gain leading separator, content = %q", got)
	}
	if !strings.Contains(got, "1 steps") {
		t.Fatalf("notice missing, content = %q", got)
	}
}

func TestNudgeResetsOnNewTurn(t *testing.T) {
	t.Parallel()

	p, clock := newNudgeProvider(t, TurnNudgeConfig{
		StepInterval:        2,
		TimeIntervalMinutes: 10,
		StepLimit:           bigSteps,
		TimeLimitMinutes:    bigSteps,
	})
	ctx := nudgeCtx("sess-reset")
	startTurn(t, p, ctx)
	advanceToStep(t, p, ctx, 1)
	callTool(t, p, ctx, "ok")
	advanceToStep(t, p, ctx, 2)
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "2 steps") {
		t.Fatalf("step 2 should carry notice, content = %q", got)
	}

	// New turn: counters and clock baseline reset.
	clock.Advance(time.Hour)
	startTurn(t, p, ctx)
	advanceToStep(t, p, ctx, 1)
	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("step 1 of new turn content = %q, want unchanged", got)
	}
	advanceToStep(t, p, ctx, 2)
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "2 steps") {
		t.Fatalf("step 2 of new turn should carry notice, content = %q", got)
	}
}

func TestNudgeWithoutSessionOrTurnStart(t *testing.T) {
	t.Parallel()

	p, clock := newNudgeProvider(t, TurnNudgeConfig{
		StepInterval:        2,
		TimeIntervalMinutes: 10,
		StepLimit:           bigSteps,
		TimeLimitMinutes:    bigSteps,
	})

	// No session in ctx: no-op.
	result := agentkit.ToolResult{Content: "ok"}
	if err := p.afterTool(context.Background(), &result); err != nil {
		t.Fatalf("afterTool without session: %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("content = %q, want unchanged", result.Content)
	}

	// No beforeStep (defensive lazy init): steps stay 0, so step-based nudges
	// never fire, but time-based ones still do.
	ctx := nudgeCtx("sess-lazy")
	if got := callTool(t, p, ctx, "ok").Content; got != "ok" {
		t.Fatalf("lazy state without steps should not nudge, content = %q", got)
	}
	clock.Advance(11 * time.Minute)
	if got := callTool(t, p, ctx, "ok").Content; !strings.Contains(got, "0 steps") {
		t.Fatalf("time-based nudge should fire on lazy state, content = %q", got)
	}
}

func TestNudgeCustomMessage(t *testing.T) {
	t.Parallel()

	p, _ := newNudgeProvider(t, TurnNudgeConfig{
		StepInterval:        1,
		TimeIntervalMinutes: bigSteps,
		StepLimit:           bigSteps,
		TimeLimitMinutes:    bigSteps,
		Message:             "WRAP UP steps={steps} elapsed={elapsed}",
	})
	ctx := nudgeCtx("sess-custom")
	startTurn(t, p, ctx)

	advanceToStep(t, p, ctx, 1)
	got := callTool(t, p, ctx, "ok").Content
	if !strings.Contains(got, "WRAP UP steps=1 elapsed=0s") {
		t.Fatalf("custom message not applied, content = %q", got)
	}
}
