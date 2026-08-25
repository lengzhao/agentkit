package headless

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

type TimerConfig struct {
	// EverySeconds is the tick interval. Required.
	EverySeconds int `json:"everySeconds"`
	// Prompt is the task text sent on every tick. Required.
	Prompt string `json:"prompt"`
	// Immediate fires the first tick at startup instead of waiting a full
	// interval. Defaults to true, so a restart does useful work right away.
	Immediate *bool `json:"immediate"`
	// MaxRuns bounds the number of ticks; 0 means run until shutdown.
	MaxRuns int `json:"maxRuns"`
	// SessionMode is fresh (default) or fixed.
	SessionMode string `json:"sessionMode"`
	// SessionID is the id used in fixed mode, and the prefix in fresh mode.
	SessionID string `json:"sessionId"`
	// Output is text (default) or json, one event object per line.
	Output string `json:"output"`
	// Stream echoes assistant deltas as they arrive.
	Stream bool `json:"stream"`
}

// Timer turns the process into a daemon that wakes on a fixed interval. Ticks
// are anchored to the start time rather than to the end of the previous turn, so
// a slow turn does not make the schedule drift; missed boundaries are skipped
// rather than queued, because a backlog of stale ticks is never what a schedule
// meant.
type Timer struct {
	interval  time.Duration
	prompt    string
	immediate bool
	maxRuns   int
	naming    sessionNamer
	emitter   *emitter
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error

	mu      sync.Mutex
	started time.Time
	runs    int
}

// NewTimer registers platform/timer: Fire the same prompt on a fixed interval.
//
// Best practices:
//   - Ticks are anchored to the start time and missed ones are skipped, so a slow turn does not make the schedule drift.
//   - Use platform/worker with a cron expression when you need calendar times rather than an interval.
func NewTimer(cfg TimerConfig) (agentkit.Platform, error) {
	if cfg.EverySeconds <= 0 {
		return nil, fmt.Errorf("platform/timer requires everySeconds > 0")
	}
	prompt := strings.TrimSpace(cfg.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("platform/timer requires a prompt")
	}
	if cfg.MaxRuns < 0 {
		return nil, fmt.Errorf("platform/timer maxRuns must not be negative")
	}
	mode, err := resolveSessionMode(cfg.SessionMode)
	if err != nil {
		return nil, fmt.Errorf("platform/timer: %w", err)
	}
	sessionID := strings.TrimSpace(cfg.SessionID)
	if sessionID == "" {
		sessionID = "timer"
	}
	immediate := true
	if cfg.Immediate != nil {
		immediate = *cfg.Immediate
	}
	return &Timer{
		interval:  time.Duration(cfg.EverySeconds) * time.Second,
		prompt:    prompt,
		immediate: immediate,
		maxRuns:   cfg.MaxRuns,
		naming:    newSessionNamer(mode, sessionID, nil),
		emitter:   newEmitter(cfg.Output, cfg.Stream),
		now:       time.Now,
		sleep:     sleepContext,
	}, nil
}

func (t *Timer) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	if err := ctx.Err(); err != nil {
		return agentkit.MessageEvent{}, err
	}
	t.mu.Lock()
	if t.started.IsZero() {
		t.started = t.now()
	}
	run := t.runs
	started := t.started
	t.mu.Unlock()

	if t.maxRuns > 0 && run >= t.maxRuns {
		slog.Info("timer reached maxRuns, shutting down", "runs", run)
		return agentkit.MessageEvent{}, io.EOF
	}

	if run > 0 || !t.immediate {
		if err := t.waitForTick(ctx, started, run); err != nil {
			return agentkit.MessageEvent{}, err
		}
	}

	t.mu.Lock()
	t.runs++
	t.mu.Unlock()

	slog.Info("timer tick", "run", run+1, "interval", t.interval.String())
	return agentkit.MessageEvent{
		SessionID:  t.naming.forRun(run),
		PlatformID: "timer",
		Message:    userMessage(t.prompt),
	}, nil
}

// waitForTick sleeps until the next boundary at or after now. Boundaries are
// computed from the start time, and any already-passed boundary is dropped with
// a log line: running a stale schedule five times back to back would be worse
// than skipping it.
func (t *Timer) waitForTick(ctx context.Context, started time.Time, run int) error {
	offset := run
	if !t.immediate {
		offset = run + 1
	}
	target := started.Add(time.Duration(offset) * t.interval)
	now := t.now()
	if !target.After(now) {
		behind := now.Sub(target)
		skipped := int(behind/t.interval) + 1
		target = target.Add(time.Duration(skipped) * t.interval)
		slog.Warn("timer fell behind, skipping missed ticks",
			"skipped", skipped,
			"behind", behind.String(),
			"next", target.UTC().Format(time.RFC3339),
		)
	}
	return t.sleep(ctx, target.Sub(now))
}

func (t *Timer) Send(_ context.Context, event agentkit.OutboundEvent) error {
	return t.emitter.send(event)
}

// SetClockForTest replaces the timer's clock and sleep so schedule behaviour can
// be asserted without real waiting. Test-only.
func (t *Timer) SetClockForTest(now func() time.Time, sleep func(context.Context, time.Duration) error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now != nil {
		t.now = now
	}
	if sleep != nil {
		t.sleep = sleep
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
