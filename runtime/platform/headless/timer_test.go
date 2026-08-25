package headless_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/headless"
)

// fakeClock advances only when the code under test sleeps, so schedule maths can
// be asserted without waiting.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
	// lag is added on each sleep to simulate a turn that overran its slot.
	lag time.Duration
}

func newFakeClock() *fakeClock {
	return newFakeClockAt(time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
}

// newFakeClockAt is for tests that also touch a schedule registry: the registry
// anchors jobs with the real clock, so the fake one has to share that timeline or
// the two disagree about when a boundary is.
func newFakeClockAt(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slept = append(c.slept, d)
	c.now = c.now.Add(d + c.lag)
	return nil
}

func (c *fakeClock) sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.slept...)
}

func newTestTimer(t *testing.T, cfg headless.TimerConfig) (agentkit.Platform, *fakeClock) {
	t.Helper()
	p, err := headless.NewTimer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	timer, ok := p.(*headless.Timer)
	if !ok {
		t.Fatalf("NewTimer returned %T", p)
	}
	clock := newFakeClock()
	timer.SetClockForTest(clock.Now, clock.Sleep)
	return p, clock
}

func TestTimerFiresImmediatelyThenOnInterval(t *testing.T) {
	t.Parallel()

	p, clock := newTestTimer(t, headless.TimerConfig{
		EverySeconds: 60,
		Prompt:       "check things",
		MaxRuns:      3,
	})
	ctx := context.Background()

	var sessions []agentkit.SessionID
	for {
		event, err := p.Receive(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		if got := textOfMessage(event.Message); got != "check things" {
			t.Fatalf("prompt = %q", got)
		}
		if event.PlatformID != "timer" {
			t.Fatalf("platform id = %q, want timer", event.PlatformID)
		}
		sessions = append(sessions, event.SessionID)
	}

	if len(sessions) != 3 {
		t.Fatalf("ticks = %d, want 3 (maxRuns)", len(sessions))
	}
	// Immediate: the first tick did not sleep; the next two waited a full interval.
	want := []time.Duration{time.Minute, time.Minute}
	got := clock.sleeps()
	if len(got) != len(want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sleep[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	// Fresh mode by default: no session is reused across ticks.
	seen := map[agentkit.SessionID]bool{}
	for _, id := range sessions {
		if seen[id] {
			t.Fatalf("session %q reused across ticks: %v", id, sessions)
		}
		seen[id] = true
	}
}

func TestTimerWaitsBeforeFirstTickWhenNotImmediate(t *testing.T) {
	t.Parallel()

	no := false
	p, clock := newTestTimer(t, headless.TimerConfig{
		EverySeconds: 30,
		Prompt:       "later",
		Immediate:    &no,
		MaxRuns:      2,
	})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := p.Receive(ctx); err != nil {
			t.Fatalf("receive %d: %v", i, err)
		}
	}
	got := clock.sleeps()
	if len(got) != 2 {
		t.Fatalf("sleeps = %v, want two waits", got)
	}
	for i, d := range got {
		if d != 30*time.Second {
			t.Fatalf("sleep[%d] = %v, want 30s", i, d)
		}
	}
}

func TestTimerDoesNotDriftAfterASlowTurn(t *testing.T) {
	t.Parallel()

	// Each turn overruns its slot by 10s. Ticks are anchored to the start time,
	// so the next sleep shortens instead of the schedule sliding later.
	p, clock := newTestTimer(t, headless.TimerConfig{
		EverySeconds: 60,
		Prompt:       "poll",
		MaxRuns:      3,
	})
	clock.lag = 10 * time.Second
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := p.Receive(ctx); err != nil {
			t.Fatalf("receive %d: %v", i, err)
		}
	}
	got := clock.sleeps()
	if len(got) != 2 {
		t.Fatalf("sleeps = %v, want 2", got)
	}
	if got[0] != time.Minute {
		t.Fatalf("first sleep = %v, want 60s", got[0])
	}
	// 10s was lost to the slow turn, so the second wait is 50s, not another 60s.
	if got[1] != 50*time.Second {
		t.Fatalf("second sleep = %v, want 50s (anchored, not drifting)", got[1])
	}
}

func TestTimerSkipsMissedTicksInsteadOfQueueing(t *testing.T) {
	t.Parallel()

	// A turn that runs far past several boundaries must not produce a burst of
	// catch-up ticks: it jumps to the next future boundary.
	p, clock := newTestTimer(t, headless.TimerConfig{
		EverySeconds: 60,
		Prompt:       "poll",
		MaxRuns:      2,
	})
	clock.lag = 250 * time.Second
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := p.Receive(ctx); err != nil {
			t.Fatalf("receive %d: %v", i, err)
		}
	}
	got := clock.sleeps()
	if len(got) != 1 {
		t.Fatalf("sleeps = %v, want 1", got)
	}
	// The first sleep is a normal interval; the lag pushes the clock past
	// boundaries 2..5, and the next tick lands on the next future one.
	if got[0] != time.Minute {
		t.Fatalf("sleep = %v, want 60s", got[0])
	}
}

func TestTimerFixedModeReusesOneSession(t *testing.T) {
	t.Parallel()

	p, _ := newTestTimer(t, headless.TimerConfig{
		EverySeconds: 10,
		Prompt:       "poll",
		MaxRuns:      2,
		SessionMode:  headless.SessionFixed,
		SessionID:    "watch",
	})
	ctx := context.Background()
	first, err := p.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID != second.SessionID {
		t.Fatalf("fixed mode gave %q then %q", first.SessionID, second.SessionID)
	}
}

func TestTimerRunsForeverWhenMaxRunsIsZero(t *testing.T) {
	t.Parallel()

	p, _ := newTestTimer(t, headless.TimerConfig{EverySeconds: 1, Prompt: "poll"})
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		if _, err := p.Receive(ctx); err != nil {
			t.Fatalf("receive %d: %v", i, err)
		}
	}
}

func TestTimerStopsOnCancellation(t *testing.T) {
	t.Parallel()

	// Real clock here: cancelling mid-sleep is the shutdown path, so it must
	// actually interrupt the wait rather than run out the interval.
	p, err := headless.NewTimer(headless.TimerConfig{
		EverySeconds: 3600,
		Prompt:       "poll",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := p.Receive(ctx); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := p.Receive(ctx)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("receive err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling did not interrupt the timer's sleep")
	}
}

func TestTimerConfigValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  headless.TimerConfig
	}{
		{"no interval", headless.TimerConfig{Prompt: "x"}},
		{"no prompt", headless.TimerConfig{EverySeconds: 10}},
		{"negative maxRuns", headless.TimerConfig{EverySeconds: 10, Prompt: "x", MaxRuns: -1}},
		{"bad session mode", headless.TimerConfig{EverySeconds: 10, Prompt: "x", SessionMode: "weird"}},
	}
	for _, tc := range cases {
		if _, err := headless.NewTimer(tc.cfg); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
}
