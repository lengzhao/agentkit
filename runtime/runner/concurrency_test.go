package runner_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/runner"
)

// recordingLoop tracks dispatch order and peak concurrency.
type recordingLoop struct {
	hold func(agentkit.LoopRequest) // optional per-dispatch delay/gate

	mu      sync.Mutex
	order   []string
	active  int
	peak    int
	entered chan struct{}
}

func (l *recordingLoop) Dispatch(_ context.Context, req agentkit.LoopRequest) error {
	label := fmt.Sprintf("%s/%s", req.Event.SessionID, textOf(req.Event.Message))

	l.mu.Lock()
	l.active++
	if l.active > l.peak {
		l.peak = l.active
	}
	l.order = append(l.order, label)
	l.mu.Unlock()

	if l.entered != nil {
		l.entered <- struct{}{}
	}
	if l.hold != nil {
		l.hold(req)
	}

	l.mu.Lock()
	l.active--
	l.mu.Unlock()
	return nil
}

func (l *recordingLoop) Steer(context.Context, agentkit.ModelMessage) error    { return nil }
func (l *recordingLoop) FollowUp(context.Context, agentkit.ModelMessage) error { return nil }
func (l *recordingLoop) TryDeliverPermission(agentkit.MessageEvent) bool      { return false }
func (l *recordingLoop) SupersedePendingForInbound(agentkit.MessageEvent)       {}

func (l *recordingLoop) snapshot() ([]string, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.order...), l.peak
}

func textOf(msg agentkit.ModelMessage) string {
	for _, part := range msg.Content {
		if part.Type == "text" {
			return part.Text
		}
	}
	return ""
}

func runToCompletion(t *testing.T, cfg runner.Config, platform agentkit.Platform, loop agentkit.Loop) {
	t.Helper()
	root, err := runner.New(cfg, runner.Deps{Platform: platform, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Run(context.Background(), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestSameSessionKeepsArrivalOrder is the invariant concurrency must not break.
// Loop serializes per session with a plain mutex, and Go mutexes are not FIFO, so
// ordering has to be enforced by the scheduler rather than assumed.
func TestSameSessionKeepsArrivalOrder(t *testing.T) {
	t.Parallel()

	const messages = 30
	events := make([]agentkit.MessageEvent, 0, messages)
	want := make([]string, 0, messages)
	for i := 0; i < messages; i++ {
		text := fmt.Sprintf("m%02d", i)
		events = append(events, userEvent("s:1", text))
		want = append(want, "s:1/"+text)
	}

	loop := &recordingLoop{hold: func(agentkit.LoopRequest) {
		// Yield so a scheduler that lost ordering has every chance to show it.
		time.Sleep(time.Millisecond)
	}}
	runToCompletion(t, runner.Config{MaxConcurrentTurns: 8}, &scriptedPlatform{events: events}, loop)

	got, peak := loop.snapshot()
	if len(got) != messages {
		t.Fatalf("dispatched %d, want %d", len(got), messages)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q\nfull: %v", i, got[i], want[i], got)
		}
	}
	if peak != 1 {
		t.Fatalf("peak concurrency within one session = %d, want 1", peak)
	}
}

// TestDistinctSessionsRunInParallel proves the feature actually engages: three
// turns that each block until all three have started can only finish together.
func TestDistinctSessionsRunInParallel(t *testing.T) {
	t.Parallel()

	const sessions = 3
	gate := make(chan struct{})
	var once sync.Once
	arrived := make(chan struct{}, sessions)

	loop := &recordingLoop{hold: func(agentkit.LoopRequest) {
		arrived <- struct{}{}
		if len(arrived) == sessions {
			once.Do(func() { close(gate) })
		}
		select {
		case <-gate:
		case <-time.After(5 * time.Second):
			// Falls through; the peak assertion below reports the failure.
		}
	}}

	events := make([]agentkit.MessageEvent, 0, sessions)
	for i := 0; i < sessions; i++ {
		events = append(events, userEvent(agentkit.SessionID(fmt.Sprintf("s:%d", i)), "go"))
	}
	runToCompletion(t, runner.Config{MaxConcurrentTurns: sessions}, &scriptedPlatform{events: events}, loop)

	if _, peak := loop.snapshot(); peak != sessions {
		t.Fatalf("peak concurrency = %d, want %d: distinct sessions should overlap", peak, sessions)
	}
}

func TestConcurrencyIsCappedByConfig(t *testing.T) {
	t.Parallel()

	loop := &recordingLoop{hold: func(agentkit.LoopRequest) {
		time.Sleep(20 * time.Millisecond)
	}}
	events := make([]agentkit.MessageEvent, 0, 12)
	for i := 0; i < 12; i++ {
		events = append(events, userEvent(agentkit.SessionID(fmt.Sprintf("s:%d", i)), "go"))
	}
	runToCompletion(t, runner.Config{MaxConcurrentTurns: 3}, &scriptedPlatform{events: events}, loop)

	got, peak := loop.snapshot()
	if len(got) != 12 {
		t.Fatalf("dispatched %d, want 12", len(got))
	}
	if peak > 3 {
		t.Fatalf("peak concurrency = %d, want at most 3", peak)
	}
	if peak < 2 {
		t.Fatalf("peak concurrency = %d, want the cap to actually be used", peak)
	}
}

// TestDefaultIsSerialExecution pins the conservative default: turns from
// different sessions share one workspace, so parallelism must be opt-in. The
// receive loop may read ahead while a turn runs, but only one turn executes at
// a time at the default concurrency cap.
func TestDefaultIsSerialExecution(t *testing.T) {
	t.Parallel()

	platform := &countingPlatform{scriptedPlatform: scriptedPlatform{events: []agentkit.MessageEvent{
		userEvent("s:1", "one"),
		userEvent("s:2", "two"),
		userEvent("s:3", "three"),
	}}}

	var maxReadAhead int
	loop := &recordingLoop{}
	loop.hold = func(agentkit.LoopRequest) {
		dispatched, _ := loop.snapshot()
		if ahead := platform.received() - len(dispatched); ahead > maxReadAhead {
			maxReadAhead = ahead
		}
		time.Sleep(5 * time.Millisecond)
	}

	runToCompletion(t, runner.Config{}, platform, loop)

	if _, peak := loop.snapshot(); peak != 1 {
		t.Fatalf("peak concurrency = %d, want 1 by default", peak)
	}
	if maxReadAhead == 0 {
		t.Fatal("expected receive to read ahead while a turn was running")
	}
}

// countingPlatform reports how many events the intake has consumed.
type countingPlatform struct {
	scriptedPlatform
	mu    sync.Mutex
	count int
}

func (p *countingPlatform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	event, err := p.scriptedPlatform.Receive(ctx)
	if err == nil {
		p.mu.Lock()
		p.count++
		p.mu.Unlock()
	}
	return event, err
}

func (p *countingPlatform) received() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}

// TestShutdownWaitsForInFlightTurns matters because a cut-off turn never records
// turn/end, leaving a session that needs crash repair on next start.
func TestShutdownWaitsForInFlightTurns(t *testing.T) {
	t.Parallel()

	var finished int
	var mu sync.Mutex
	loop := &recordingLoop{hold: func(agentkit.LoopRequest) {
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		finished++
		mu.Unlock()
	}}

	events := make([]agentkit.MessageEvent, 0, 4)
	for i := 0; i < 4; i++ {
		events = append(events, userEvent(agentkit.SessionID(fmt.Sprintf("s:%d", i)), "go"))
	}
	runToCompletion(t, runner.Config{MaxConcurrentTurns: 4}, &scriptedPlatform{events: events}, loop)

	// Run returned, so every dispatched turn must already have completed.
	mu.Lock()
	defer mu.Unlock()
	if finished != 4 {
		t.Fatalf("completed turns = %d, want 4: Run returned before turns finished", finished)
	}
}

func TestRejectsNegativeConcurrency(t *testing.T) {
	t.Parallel()

	if _, err := runner.New(runner.Config{MaxConcurrentTurns: -1}, runner.Deps{
		Platform: &scriptedPlatform{},
		Loop:     &recordingLoop{},
	}); err == nil {
		t.Fatal("expected an error for negative maxConcurrentTurns")
	}
}

// TestPanicInOneSessionDoesNotStallOthers combines panic isolation with
// concurrency: the slot a panicking turn held has to come back.
func TestPanicInOneSessionDoesNotStallOthers(t *testing.T) {
	t.Parallel()

	platform := &scriptedPlatform{events: []agentkit.MessageEvent{
		userEvent("s:1", "panics"),
		userEvent("s:2", "works"),
		userEvent("s:3", "works"),
	}}
	root, err := runner.New(runner.Config{MaxConcurrentTurns: 2}, runner.Deps{
		Platform: platform,
		Loop:     &panickyLoop{},
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- root.Run(context.Background(), nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run stalled: a panicking turn leaked its concurrency slot")
	}
}
