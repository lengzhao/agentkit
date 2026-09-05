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
	"github.com/lengzhao/agentkit/runtime/session"
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

func startAsync(ctx context.Context, rt capschedule.Runtime, fn func(context.Context, agentkit.MessageEvent) error) {
	go func() {
		_ = rt.Start(ctx, func(ctx context.Context, event agentkit.MessageEvent) error {
			return fn(ctx, event)
		})
	}()
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
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- textOfMessage(event.Message)
		cancel()
		return nil
	})

	select {
	case prompt := <-got:
		if !strings.Contains(prompt, "poll now") {
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
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- textOfMessage(event.Message)
		cancel()
		return nil
	})

	if _, err := registry.Add(ctx, capschedule.Job{
		Kind:    capschedule.KindCron,
		Cron:    "*/5 * * * *",
		Prompt:  "the agent's own follow-up",
		LastRun: clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case prompt := <-got:
		if !strings.Contains(prompt, "the agent's own follow-up") {
			t.Fatalf("prompt = %q", prompt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent job")
	}
}

func TestCronFiresWithStoredDeliverySession(t *testing.T) {
	t.Parallel()

	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{PollSeconds: 60})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan agentkit.MessageEvent, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- event
		cancel()
		return nil
	})

	if _, err := registry.Add(ctx, capschedule.Job{
		Kind:              capschedule.KindCron,
		Cron:              "*/5 * * * *",
		Prompt:            "remind",
		LastRun:           clock.Now(),
		DeliverySessionID: "chat-api:default:t:conv_1",
		PlatformID:        "chat-api",
		UserID:            "user-1",
		AgentID:           "assistant",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-got:
		if session.ConversationFromEvent(event) == "chat-api:default:t:conv_1" {
			t.Fatalf("schedule fire should use side session, got delivery conversation %q", session.ConversationFromEvent(event))
		}
		if session.InboundDeliveryID(event) != "chat-api:default:t:conv_1" {
			t.Fatalf("delivery = %q", session.InboundDeliveryID(event))
		}
		if event.PlatformID != "chat-api" {
			t.Fatalf("platform = %q", event.PlatformID)
		}
		if event.UserID != "user-1" {
			t.Fatalf("user = %q", event.UserID)
		}
		if event.AgentID != "assistant" {
			t.Fatalf("agent = %q", event.AgentID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for delivery-routed cron fire")
	}
}

func TestCronReuseModeUsesDeliverySession(t *testing.T) {
	t.Parallel()

	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{
		SessionMode: "reuse",
		PollSeconds: 60,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan agentkit.MessageEvent, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- event
		cancel()
		return nil
	})

	if _, err := registry.Add(ctx, capschedule.Job{
		Kind:              capschedule.KindCron,
		Cron:              "*/5 * * * *",
		Prompt:            "remind",
		LastRun:           clock.Now(),
		DeliverySessionID: "chat-api:default:t:conv_reuse",
		PlatformID:        "chat-api",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-got:
		if session.ConversationFromEvent(event) != "chat-api:default:t:conv_reuse" {
			t.Fatalf("reuse conversation = %q, want delivery session", session.ConversationFromEvent(event))
		}
		meta, ok := event.Metadata["schedule"].(map[string]any)
		if !ok {
			t.Fatal("missing schedule metadata")
		}
		if meta["sessionMode"] != "reuse" {
			t.Fatalf("sessionMode = %v", meta["sessionMode"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reuse cron fire")
	}
}

func TestCronStatelessModeUsesPerJobSession(t *testing.T) {
	t.Parallel()

	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{
		SessionMode: "stateless",
		PollSeconds: 60,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan agentkit.MessageEvent, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- event
		cancel()
		return nil
	})

	if _, err := registry.Add(ctx, capschedule.Job{
		ID:                "agent-9",
		Kind:              capschedule.KindCron,
		Cron:              "*/5 * * * *",
		Prompt:            "remind",
		LastRun:           clock.Now(),
		DeliverySessionID: "chat-api:default:t:conv_1",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-got:
		if !strings.HasPrefix(string(session.ConversationFromEvent(event)), "schedule:agent-9:") {
			t.Fatalf("stateless conversation = %q", session.ConversationFromEvent(event))
		}
		meta, ok := event.Metadata["schedule"].(map[string]any)
		if !ok {
			t.Fatal("missing schedule metadata")
		}
		if meta["sessionMode"] != "stateless" {
			t.Fatalf("sessionMode = %v", meta["sessionMode"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stateless cron fire")
	}
}

func TestCronMarksDelayJobFiredAfterFire(t *testing.T) {
	t.Parallel()

	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{PollSeconds: 60})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fireAt := clock.Now().Add(5 * time.Minute)
	if _, err := registry.Add(ctx, capschedule.Job{
		Kind:   capschedule.KindDelay,
		FireAt: fireAt,
		Prompt: "remind once",
	}); err != nil {
		t.Fatal(err)
	}

	got := make(chan struct{}, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		if strings.Contains(textOfMessage(event.Message), "remind once") {
			got <- struct{}{}
			cancel()
		}
		return nil
	})

	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for one-shot cron fire")
	}

	jobs, err := registry.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs after one-shot fire = %+v, want fired history retained", jobs)
	}
	if !jobs[0].Fired || jobs[0].FiredAt.IsZero() {
		t.Fatalf("fired state = %+v", jobs[0])
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
	go func() {
		done <- rt.Start(ctx, func(context.Context, agentkit.MessageEvent) error { return nil })
	}()
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
