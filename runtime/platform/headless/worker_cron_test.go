package headless_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/workspace"
	pluginschedule "github.com/lengzhao/agentkit/plugins/schedule"
	"github.com/lengzhao/agentkit/runtime/platform/headless"
)

func newCronWorker(t *testing.T, cfg headless.WorkerConfig) (agentkit.Platform, capschedule.Registry, *fakeClock) {
	t.Helper()
	registry, err := pluginschedule.NewFile(pluginschedule.FileConfig{Path: "schedule.json"},
		pluginschedule.FileDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	p, err := headless.NewWorker(cfg, headless.WorkerDeps{Schedule: registry})
	if err != nil {
		t.Fatal(err)
	}
	worker, ok := p.(*headless.Worker)
	if !ok {
		t.Fatalf("NewWorker returned %T", p)
	}
	clock := newFakeClockAt(time.Now().UTC().Truncate(time.Minute))
	worker.SetClockForTest(clock.Now, clock.Sleep)
	return p, registry, clock
}

// TestCronTaskRequiresARegistry keeps a cron typo from silently never firing.
func TestCronTaskRequiresARegistry(t *testing.T) {
	t.Parallel()

	_, err := headless.NewWorker(headless.WorkerConfig{
		Tasks: []headless.TaskSpec{{Prompt: "nightly", Cron: "@daily"}},
	}, headless.WorkerDeps{})
	if err == nil {
		t.Fatal("expected an error: a cron task without a schedule registry can never run")
	}
}

func TestCronTaskWithoutPromptIsRejected(t *testing.T) {
	t.Parallel()

	registry, err := pluginschedule.NewFile(pluginschedule.FileConfig{Path: "schedule.json"},
		pluginschedule.FileDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := headless.NewWorker(headless.WorkerConfig{
		Tasks: []headless.TaskSpec{{Cron: "@daily"}},
	}, headless.WorkerDeps{Schedule: registry}); err == nil {
		t.Fatal("expected an error for a cron task with no prompt")
	}
}

func TestInvalidCronIsRejectedAtStartup(t *testing.T) {
	t.Parallel()

	registry, err := pluginschedule.NewFile(pluginschedule.FileConfig{Path: "schedule.json"},
		pluginschedule.FileDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := headless.NewWorker(headless.WorkerConfig{
		Tasks: []headless.TaskSpec{{Prompt: "x", Cron: "99 * * * *"}},
	}, headless.WorkerDeps{Schedule: registry}); err == nil {
		t.Fatal("expected startup to reject an invalid cron expression")
	}
}

// TestWorkerRunsStartupTasksBeforeSchedule pins the ordering: a resident worker
// should do its immediate work before settling into the calendar.
func TestWorkerRunsStartupTasksBeforeSchedule(t *testing.T) {
	t.Parallel()

	p, _, clock := newCronWorker(t, headless.WorkerConfig{
		Tasks: []headless.TaskSpec{
			{Prompt: "startup one"},
			{Prompt: "startup two"},
			{Prompt: "hourly job", Cron: "0 * * * *"},
		},
	})
	ctx := context.Background()

	for _, want := range []string{"startup one", "startup two"} {
		event, err := p.Receive(ctx)
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		if got := textOfMessage(event.Message); got != want {
			t.Fatalf("task = %q, want %q", got, want)
		}
	}
	if len(clock.sleeps()) != 0 {
		t.Fatalf("startup tasks should not wait, slept %v", clock.sleeps())
	}

	// Now it waits for the schedule rather than reporting EOF.
	event, err := p.Receive(ctx)
	if err != nil {
		t.Fatalf("scheduled receive: %v", err)
	}
	if got := textOfMessage(event.Message); got != "hourly job" {
		t.Fatalf("scheduled task = %q", got)
	}
	if len(clock.sleeps()) == 0 {
		t.Fatal("expected the worker to wait for the boundary")
	}
}

// TestBatchModeStillReportsEOF proves cron support did not turn every worker into
// a daemon.
func TestBatchModeStillReportsEOF(t *testing.T) {
	t.Parallel()

	p, err := headless.NewWorker(headless.WorkerConfig{
		Tasks: []headless.TaskSpec{{Prompt: "one"}, {Prompt: "two"}},
	}, headless.WorkerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := p.Receive(ctx); err != nil {
			t.Fatalf("receive %d: %v", i, err)
		}
	}
	if _, err := p.Receive(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want EOF", err)
	}
}

func TestCronJobFiresOnItsSchedule(t *testing.T) {
	t.Parallel()

	p, _, clock := newCronWorker(t, headless.WorkerConfig{
		Tasks:       []headless.TaskSpec{{ID: "poll", Prompt: "poll now", Cron: "0 * * * *"}},
		PollSeconds: 3600,
	})
	ctx := context.Background()
	start := clock.Now()
	// "0 * * * *" anchored at startup fires at the next top of the hour, not now.
	wantFirst := start.Truncate(time.Hour).Add(time.Hour)

	event, err := p.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if got := textOfMessage(event.Message); got != "poll now" {
		t.Fatalf("prompt = %q", got)
	}
	if got := clock.Now(); !got.Equal(wantFirst) {
		t.Fatalf("fired at %s, want %s", got.Format(time.RFC3339), wantFirst.Format(time.RFC3339))
	}

	// And again the following hour, not immediately.
	if _, err := p.Receive(ctx); err != nil {
		t.Fatalf("second receive: %v", err)
	}
	if got, want := clock.Now(), wantFirst.Add(time.Hour); !got.Equal(want) {
		t.Fatalf("second fire at %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestSeveralJobsDueAtOnceAreDeliveredIndividually(t *testing.T) {
	t.Parallel()

	p, _, _ := newCronWorker(t, headless.WorkerConfig{
		Tasks: []headless.TaskSpec{
			{ID: "a", Prompt: "job a", Cron: "0 * * * *"},
			{ID: "b", Prompt: "job b", Cron: "0 * * * *"},
		},
		PollSeconds: 3600,
	})
	ctx := context.Background()

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		event, err := p.Receive(ctx)
		if err != nil {
			t.Fatalf("receive %d: %v", i, err)
		}
		seen[textOfMessage(event.Message)] = true
	}
	if !seen["job a"] || !seen["job b"] {
		t.Fatalf("both jobs should fire on the same boundary, got %v", seen)
	}
}

// TestAgentAddedJobIsPickedUpWithoutRestart is the payoff of sharing one registry
// between the tool and the platform.
func TestAgentAddedJobIsPickedUpWithoutRestart(t *testing.T) {
	t.Parallel()

	p, registry, clock := newCronWorker(t, headless.WorkerConfig{
		Tasks:       []headless.TaskSpec{{Prompt: "startup"}},
		PollSeconds: 60,
	})
	ctx := context.Background()

	if _, err := p.Receive(ctx); err != nil {
		t.Fatalf("startup task: %v", err)
	}

	// The agent schedules follow-up work mid-run.
	if _, err := registry.Add(ctx, capschedule.Job{
		Cron:    "*/5 * * * *",
		Prompt:  "the agent's own follow-up",
		LastRun: clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	event, err := p.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if got := textOfMessage(event.Message); got != "the agent's own follow-up" {
		t.Fatalf("prompt = %q, want the agent's job", got)
	}
}

func TestCronWorkerStopsOnCancellation(t *testing.T) {
	t.Parallel()

	registry, err := pluginschedule.NewFile(pluginschedule.FileConfig{Path: "schedule.json"},
		pluginschedule.FileDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	// Real clock: cancelling mid-wait is the shutdown path.
	p, err := headless.NewWorker(headless.WorkerConfig{
		Tasks:       []headless.TaskSpec{{Prompt: "daily", Cron: "0 0 1 1 *"}},
		PollSeconds: 3600,
	}, headless.WorkerDeps{Schedule: registry})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := p.Receive(ctx)
		done <- err
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

// TestTaskSpecAcceptsBareStrings keeps existing `tasks: ["..."]` configs working.
func TestTaskSpecAcceptsBareStrings(t *testing.T) {
	t.Parallel()

	var specs []headless.TaskSpec
	raw := []byte(`["plain task", {"prompt":"scheduled task","cron":"@daily"}]`)
	if err := json.Unmarshal(raw, &specs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2", len(specs))
	}
	if specs[0].Prompt != "plain task" || specs[0].Cron != "" {
		t.Fatalf("bare string decoded as %+v", specs[0])
	}
	if specs[1].Prompt != "scheduled task" || specs[1].Cron != "@daily" {
		t.Fatalf("object decoded as %+v", specs[1])
	}
}

// cronWorkerRaceGuard exercises concurrent Send while the cron loop runs, since
// Runner may emit from several turns at once.
func TestWorkerSendIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	p, err := headless.NewWorker(headless.WorkerConfig{Prompt: "x", Output: headless.OutputJSON},
		headless.WorkerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Send(context.Background(), agentkit.OutboundEvent{
				SessionID: "s:1",
				Type:      "error",
				Data:      []byte(`{"error":"concurrent"}`),
			})
		}()
	}
	wg.Wait()
}
