package schedule_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/workspace"
	pluginschedule "github.com/lengzhao/agentkit/plugins/schedule"
)

type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
}

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
	c.now = c.now.Add(d)
	return nil
}

func (c *fakeClock) sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.slept...)
}

func newCronRuntime(t *testing.T, cfg pluginschedule.CronConfig) (capschedule.Runtime, capschedule.Registry, *fakeClock) {
	t.Helper()
	registry, err := pluginschedule.NewFile(pluginschedule.FileConfig{Path: "schedule.json"},
		pluginschedule.FileDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := pluginschedule.NewCron(cfg, pluginschedule.CronDeps{Schedule: registry})
	if err != nil {
		t.Fatal(err)
	}
	cron, ok := rt.(*pluginschedule.Cron)
	if !ok {
		t.Fatalf("NewCron returned %T", rt)
	}
	clock := newFakeClockAt(time.Now().UTC().Truncate(time.Minute))
	cron.SetClockForTest(clock.Now, clock.Sleep)
	return rt, registry, clock
}

func textOfMessage(msg agentkit.ModelMessage) string {
	var b strings.Builder
	for _, part := range msg.Content {
		b.WriteString(part.Text)
	}
	return b.String()
}

func TestCronJobFiresOnItsSchedule(t *testing.T) {
	t.Parallel()

	rt, _, clock := newCronRuntime(t, pluginschedule.CronConfig{
		Jobs: []pluginschedule.CronJobSpec{{
			ID: "poll", Cron: "0 * * * *", Prompt: "poll now",
		}},
		PollSeconds: 3600,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := clock.Now()
	wantFirst := start.Truncate(time.Hour).Add(time.Hour)
	got := make(chan string, 1)
	go func() {
		_ = rt.Start(ctx, func(_ context.Context, event agentkit.MessageEvent) error {
			got <- textOfMessage(event.Message)
			cancel()
			return nil
		})
	}()

	select {
	case prompt := <-got:
		if prompt != "poll now" {
			t.Fatalf("prompt = %q", prompt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cron fire")
	}
	if got := clock.Now(); !got.Equal(wantFirst) {
		t.Fatalf("fired at %s, want %s", got.Format(time.RFC3339), wantFirst.Format(time.RFC3339))
	}
}

func TestAgentAddedJobIsPickedUpWithoutRestart(t *testing.T) {
	t.Parallel()

	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{PollSeconds: 60})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan string, 1)
	go func() {
		_ = rt.Start(ctx, func(_ context.Context, event agentkit.MessageEvent) error {
			got <- textOfMessage(event.Message)
			cancel()
			return nil
		})
	}()

	if _, err := registry.Add(ctx, capschedule.Job{
		Cron:    "*/5 * * * *",
		Prompt:  "the agent's own follow-up",
		LastRun: clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case prompt := <-got:
		if prompt != "the agent's own follow-up" {
			t.Fatalf("prompt = %q", prompt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent job")
	}
}

func TestCronStopsOnCancellation(t *testing.T) {
	t.Parallel()

	registry, err := pluginschedule.NewFile(pluginschedule.FileConfig{Path: "schedule.json"},
		pluginschedule.FileDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := pluginschedule.NewCron(pluginschedule.CronConfig{
		Jobs:        []pluginschedule.CronJobSpec{{ID: "daily", Cron: "0 0 1 1 *", Prompt: "daily"}},
		PollSeconds: 3600,
	}, pluginschedule.CronDeps{Schedule: registry})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rt.Start(ctx, func(context.Context, agentkit.MessageEvent) error { return nil }) }()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling did not interrupt the cron wait")
	}
}
