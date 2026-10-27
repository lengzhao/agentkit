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
	pluginschedule "github.com/lengzhao/agentkit/plugins/schedule"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rtschedule "github.com/lengzhao/agentkit/runtime/schedule"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
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
		pluginschedule.FileDeps{FS: testFS(t, rtworkspace.Static(t.TempDir())), Engine: rtschedule.Engine{}})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := pluginschedule.NewCron(cfg, pluginschedule.CronDeps{Schedule: registry, Engine: rtschedule.Engine{}})
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

func cronImpl(t *testing.T, rt capschedule.Runtime) *pluginschedule.Cron {
	t.Helper()
	cron, ok := rt.(*pluginschedule.Cron)
	if !ok {
		t.Fatalf("NewCron returned %T", rt)
	}
	return cron
}

func waitCronLoop(t *testing.T, cron *pluginschedule.Cron) {
	t.Helper()
	ready := make(chan struct{})
	cron.SetLoopHookForTest(func() {
		select {
		case <-ready:
		default:
			close(ready)
		}
	})
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cron loop")
	}
	cron.SetLoopHookForTest(nil)
}

func startAsync(ctx context.Context, rt capschedule.Runtime, fn func(context.Context, agentkit.MessageEvent) error) {
	go func() {
		_ = rt.Start(ctx, func(ctx context.Context, event agentkit.MessageEvent) error {
			return fn(ctx, event)
		})
	}()
}

func waitJobFired(t *testing.T, registry capschedule.Registry, ctx context.Context) capschedule.Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := registry.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) == 1 && jobs[0].Fired {
			return jobs[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	jobs, _ := registry.List(ctx)
	t.Fatalf("jobs after one-shot fire = %+v, want fired history retained", jobs)
	return capschedule.Job{}
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
	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{PollSeconds: 1})
	cron := cronImpl(t, rt)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan string, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- textOfMessage(event.Message)
		cancel()
		return nil
	})
	waitCronLoop(t, cron)

	if _, err := registry.Add(ctx, capschedule.Job{
		Kind:    capschedule.KindCron,
		Cron:    "*/5 * * * *",
		Prompt:  "the agent's own follow-up",
		LastRun: clock.Now().Add(-15 * time.Minute),
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
	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{PollSeconds: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := registry.Add(ctx, capschedule.Job{
		Kind:    capschedule.KindCron,
		Cron:    "*/5 * * * *",
		Prompt:  "remind",
		LastRun: clock.Now().Add(-15 * time.Minute),
		Route:   rctx.SessionRoute("chat-api", "chat-api:default:t:conv_1"),
		Actor:   agentkit.ActorRef{UserID: "user-1"},
		AgentID: "assistant",
	}); err != nil {
		t.Fatal(err)
	}

	got := make(chan agentkit.MessageEvent, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- event
		cancel()
		return nil
	})

	select {
	case event := <-got:
		if rctx.ConversationFromEvent(event) == "chat-api:default:t:conv_1" {
			t.Fatalf("schedule fire should use side session, got delivery conversation %q", rctx.ConversationFromEvent(event))
		}
		if rctx.InboundDeliveryID(event) != "chat-api:default:t:conv_1" {
			t.Fatalf("delivery = %q", rctx.InboundDeliveryID(event))
		}
		if event.Envelope.Route.Instance != "chat-api" {
			t.Fatalf("platform = %q", event.Envelope.Route.Instance)
		}
		if event.Envelope.Actor.UserID != "user-1" {
			t.Fatalf("user = %q", event.Envelope.Actor.UserID)
		}
		if event.Envelope.AgentID != "assistant" {
			t.Fatalf("agent = %q", event.Envelope.AgentID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for delivery-routed cron fire")
	}
}

func TestCronReuseModeUsesDeliverySession(t *testing.T) {
	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{
		SessionMode: "reuse",
		PollSeconds: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := registry.Add(ctx, capschedule.Job{
		Kind:    capschedule.KindCron,
		Cron:    "*/5 * * * *",
		Prompt:  "remind",
		LastRun: clock.Now().Add(-15 * time.Minute),
		Route:   rctx.SessionRoute("chat-api", "chat-api:default:t:conv_reuse"),
	}); err != nil {
		t.Fatal(err)
	}

	got := make(chan agentkit.MessageEvent, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- event
		cancel()
		return nil
	})

	select {
	case event := <-got:
		if rctx.ConversationFromEvent(event) != "chat-api:default:t:conv_reuse" {
			t.Fatalf("reuse conversation = %q, want delivery session", rctx.ConversationFromEvent(event))
		}
		meta, ok := event.Envelope.Metadata["schedule"].(map[string]any)
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
	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{
		SessionMode: "stateless",
		PollSeconds: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := registry.Add(ctx, capschedule.Job{
		ID:      "agent-9",
		Kind:    capschedule.KindCron,
		Cron:    "*/5 * * * *",
		Prompt:  "remind",
		LastRun: clock.Now().Add(-15 * time.Minute),
		Route:   rctx.SessionRoute("chat-api", "chat-api:default:t:conv_1"),
	}); err != nil {
		t.Fatal(err)
	}

	got := make(chan agentkit.MessageEvent, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- event
		cancel()
		return nil
	})

	select {
	case event := <-got:
		if !strings.HasPrefix(string(rctx.ConversationFromEvent(event)), "schedule:agent-9:") {
			t.Fatalf("stateless conversation = %q", rctx.ConversationFromEvent(event))
		}
		meta, ok := event.Envelope.Metadata["schedule"].(map[string]any)
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

	_ = waitJobFired(t, registry, ctx)
}

func TestCronStopsOnCancellation(t *testing.T) {
	t.Parallel()

	registry, err := pluginschedule.NewFile(pluginschedule.FileConfig{Path: "schedule.json"},
		pluginschedule.FileDeps{FS: testFS(t, rtworkspace.Static(t.TempDir())), Engine: rtschedule.Engine{}})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := pluginschedule.NewCron(pluginschedule.CronConfig{
		Jobs:        []pluginschedule.CronJobSpec{{ID: "daily", Cron: "0 0 1 1 *", Prompt: "daily"}},
		PollSeconds: 3600,
	}, pluginschedule.CronDeps{Schedule: registry, Engine: rtschedule.Engine{}})
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

func TestCronReuseKeepsAdapterInstanceNamespace(t *testing.T) {
	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{SessionMode: "reuse", PollSeconds: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "chat-api", Instance: "chat-api1", ChannelID: "default", ThreadID: "conv", ReplyTo: "old-message"})
	if _, err := registry.Add(ctx, capschedule.Job{Kind: capschedule.KindCron, Prompt: "remind", Cron: "*/5 * * * *", LastRun: clock.Now().Add(-15 * time.Minute), Route: route, Actor: agentkit.ActorRef{UserID: "U1", Name: "User"}, AgentID: "assistant"}); err != nil {
		t.Fatal(err)
	}
	got := make(chan agentkit.MessageEvent, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error { got <- event; cancel(); return nil })
	select {
	case event := <-got:
		if event.Envelope.Route.Platform != "chat-api" || event.Envelope.Route.DestinationID() != "chat-api1" {
			t.Fatalf("route = %+v", event.Envelope.Route)
		}
		if event.Envelope.Conversation != "chat-api1:default:t:conv" {
			t.Fatalf("conversation = %q", event.Envelope.Conversation)
		}
		if event.Envelope.Actor.Name != "User" || event.Envelope.AgentID != "assistant" {
			t.Fatalf("attribution = %+v", event.Envelope)
		}
		if rctx.RouteReplyTo(event.Envelope.Route) != "" {
			t.Fatal("schedule restored an ephemeral anchor")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("schedule did not fire")
	}
}

// A failing fire (e.g. a script with a non-zero exit) must not stop the
// calendar: later jobs in the same process still fire. Before the retry loop,
// one bad script ended every future fire silently.
func TestCronKeepsSchedulingAfterFailedFire(t *testing.T) {
	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{PollSeconds: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Two cron jobs due now. The first submit fails; the second must still run.
	for _, id := range []string{"a-fails", "b-succeeds"} {
		if _, err := registry.Add(ctx, capschedule.Job{
			ID:      id,
			Kind:    capschedule.KindCron,
			Cron:    "*/5 * * * *",
			Prompt:  id,
			LastRun: clock.Now().Add(-15 * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var seen []string
	done := make(chan struct{})
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		text := textOfMessage(event.Message)
		mu.Lock()
		seen = append(seen, text)
		n := len(seen)
		mu.Unlock()
		if strings.Contains(text, "a-fails") {
			return errors.New("submit refused")
		}
		if n >= 2 {
			select {
			case <-done:
			default:
				close(done)
			}
		}
		return nil
	})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		mu.Lock()
		got := append([]string(nil), seen...)
		mu.Unlock()
		t.Fatalf("scheduler stopped after a failed fire; submitted = %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 2 || !strings.Contains(seen[1], "b-succeeds") {
		t.Fatalf("submitted = %v, want the second job to fire after the failure", seen)
	}
}

// A registry read error is retried instead of ending the scheduler.
func TestCronRetriesDueErrors(t *testing.T) {
	flaky := &flakyRegistry{failures: 2}
	rt, err := pluginschedule.NewCron(pluginschedule.CronConfig{PollSeconds: 1},
		pluginschedule.CronDeps{Schedule: flaky, Engine: rtschedule.Engine{}})
	if err != nil {
		t.Fatal(err)
	}
	cron := cronImpl(t, rt)
	clock := newFakeClockAt(time.Now().UTC().Truncate(time.Minute))
	cron.SetClockForTest(clock.Now, clock.Sleep)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fired := make(chan struct{})
	startAsync(ctx, rt, func(_ context.Context, _ agentkit.MessageEvent) error {
		select {
		case <-fired:
		default:
			close(fired)
		}
		cancel()
		return nil
	})

	// Once the flaky registry starts answering, a due job must still reach submit.
	flaky.mu.Lock()
	flaky.jobs = []capschedule.Job{{
		ID: "later", Kind: capschedule.KindCron, Cron: "*/5 * * * *",
		Prompt: "later", LastRun: clock.Now().Add(-15 * time.Minute),
	}}
	flaky.mu.Unlock()

	waitFor(t, 3*time.Second, func() bool {
		select {
		case <-fired:
			return true
		default:
			return false
		}
	}, "cron never recovered from a Due error")
}

// flakyRegistry fails the first N Due calls, then behaves like an empty registry.
type flakyRegistry struct {
	mu       sync.Mutex
	failures int
	jobs     []capschedule.Job
}

func (r *flakyRegistry) List(context.Context) ([]capschedule.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]capschedule.Job(nil), r.jobs...), nil
}

func (r *flakyRegistry) Add(_ context.Context, job capschedule.Job) (capschedule.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs = append(r.jobs, job)
	return job, nil
}

func (r *flakyRegistry) Remove(context.Context, string) (bool, error) { return false, nil }

func (r *flakyRegistry) SyncSource(context.Context, string, []capschedule.Job) error { return nil }

func (r *flakyRegistry) Due(_ context.Context, _ time.Time) ([]capschedule.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failures > 0 {
		r.failures--
		return nil, errors.New("registry temporarily unreadable")
	}
	due := r.jobs
	r.jobs = nil
	return due, nil
}

func (r *flakyRegistry) MarkFired(context.Context, string) error { return nil }

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

// A config job with a delivery target must route exactly like an agent-created
// job. Without this the fire degraded to the synthetic "schedule" platform and
// the answer could never reach the user.
func TestConfigJobRoutesToDeliverTo(t *testing.T) {
	t.Parallel()

	rt, _, clock := newCronRuntime(t, pluginschedule.CronConfig{
		Jobs: []pluginschedule.CronJobSpec{{
			ID:        "daily",
			Cron:      "*/5 * * * *",
			Prompt:    "morning report",
			DeliverTo: "lark:oc_demo:u:ou_owner",
			Owner:     "ou_owner",
			AgentID:   "claude",
		}},
		PollSeconds: 3600,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// SyncSource stamps LastRun at creation, so the first fire lands on the next
	// real boundary; the fake clock advances inside the scheduler's own wait.
	_ = clock

	got := make(chan agentkit.MessageEvent, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- event
		cancel()
		return nil
	})

	select {
	case event := <-got:
		if event.Envelope.Route.Instance != "lark" {
			t.Fatalf("instance = %q, want lark", event.Envelope.Route.Instance)
		}
		if rctx.InboundDeliveryID(event) != "lark:oc_demo:u:ou_owner" {
			t.Fatalf("delivery = %q", rctx.InboundDeliveryID(event))
		}
		if event.Envelope.Actor.UserID != "ou_owner" {
			t.Fatalf("owner = %q", event.Envelope.Actor.UserID)
		}
		if event.Envelope.AgentID != "claude" {
			t.Fatalf("agent = %q", event.Envelope.AgentID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for config job fire")
	}
}

// The default preamble must not name a tool: hosts without a send tool would
// otherwise be told to call something that does not exist. promptTemplate
// overrides it for hosts that do.
func TestPromptPreambleMentionsNoToolByDefault(t *testing.T) {
	t.Parallel()

	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{PollSeconds: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := registry.Add(ctx, capschedule.Job{
		ID:      "note",
		Kind:    capschedule.KindCron,
		Cron:    "*/5 * * * *",
		Prompt:  "summarize today",
		LastRun: clock.Now().Add(-15 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	got := make(chan string, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- textOfMessage(event.Message)
		cancel()
		return nil
	})

	select {
	case prompt := <-got:
		if strings.Contains(prompt, " send ") {
			t.Fatalf("default prompt still instructs a send tool: %q", prompt)
		}
		if !strings.Contains(prompt, "summarize today") {
			t.Fatalf("job prompt missing: %q", prompt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cron fire")
	}
}

func TestPromptTemplateOverride(t *testing.T) {
	t.Parallel()

	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{
		PollSeconds:    1,
		PromptTemplate: "定时任务触发。用 tool/send 回复用户。",
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := registry.Add(ctx, capschedule.Job{
		ID:      "note",
		Kind:    capschedule.KindCron,
		Cron:    "*/5 * * * *",
		Prompt:  "summarize today",
		LastRun: clock.Now().Add(-15 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	got := make(chan string, 1)
	startAsync(ctx, rt, func(_ context.Context, event agentkit.MessageEvent) error {
		got <- textOfMessage(event.Message)
		cancel()
		return nil
	})

	select {
	case prompt := <-got:
		if !strings.Contains(prompt, "tool/send") {
			t.Fatalf("promptTemplate not applied: %q", prompt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cron fire")
	}
}

// The retry wait must not follow pollSeconds: an hourly idle poll would stall
// every other job for an hour after one failed fire.
func TestRecoverBackoffIsBoundedByItsOwnCeiling(t *testing.T) {
	t.Parallel()

	rt, _, clock := newCronRuntime(t, pluginschedule.CronConfig{PollSeconds: 3600})
	cron := cronImpl(t, rt)
	ctx := context.Background()

	before := clock.Now()
	if err := cron.Recover(ctx, "fire", errors.New("boom"), "job-1"); err != nil {
		t.Fatal(err)
	}
	waited := clock.Now().Sub(before)
	if waited > time.Minute {
		t.Fatalf("backoff = %s, want at most a minute regardless of pollSeconds", waited)
	}
	if waited <= 0 {
		t.Fatalf("expected a non-zero backoff, got %s", waited)
	}

	// Repeated failures back off further, still bounded.
	first := waited
	before = clock.Now()
	if err := cron.Recover(ctx, "fire", errors.New("boom"), "job-1"); err != nil {
		t.Fatal(err)
	}
	second := clock.Now().Sub(before)
	if second < first {
		t.Fatalf("backoff shrank: %s then %s", first, second)
	}
	for i := 0; i < 20; i++ {
		_ = cron.Recover(ctx, "fire", errors.New("boom"), "job-1")
	}
	before = clock.Now()
	_ = cron.Recover(ctx, "fire", errors.New("boom"), "job-1")
	if got := clock.Now().Sub(before); got > time.Minute {
		t.Fatalf("backoff exceeded ceiling: %s", got)
	}
}
