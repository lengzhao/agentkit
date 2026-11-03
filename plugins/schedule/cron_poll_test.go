package schedule_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	pluginschedule "github.com/lengzhao/agentkit/plugins/schedule"
)

// Mutate the durable registry after Start has calculated its first sleep.
// The injected clock exercises dispatch without waiting in real time.
func TestCronRechecksDynamicJobsWithinPollInterval(t *testing.T) {
	for _, scenario := range []string{"add", "update", "remove", "recurring"} {
		t.Run(scenario, func(t *testing.T) {
			rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{PollSeconds: 30})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			start := clock.Now()
			add := func(job capschedule.Job) {
				t.Helper()
				if _, err := registry.Add(ctx, job); err != nil {
					t.Fatal(err)
				}
			}
			originalFireAt := start.Add(24 * time.Hour)
			if scenario == "remove" {
				// Keep running past the removed job's deadline. A stale cached
				// copy must not dispatch before the sentinel job at one minute.
				originalFireAt = start.Add(45 * time.Second)
			}
			add(capschedule.Job{ID: "far", Kind: capschedule.KindAt, FireAt: originalFireAt, Prompt: "far"})
			sleeps, dispatched := 0, 0
			cronImpl(t, rt).SetClockForTest(clock.Now, func(ctx context.Context, d time.Duration) error {
				sleeps++
				if sleeps == 1 {
					job := capschedule.Job{ID: "early", Kind: capschedule.KindAt, FireAt: start.Add(time.Minute), Prompt: "early"}
					switch scenario {
					case "update":
						job.ID = "far"
					case "remove":
						removed, err := registry.Remove(ctx, "far")
						if err != nil || !removed {
							t.Fatalf("Remove = %v, %v", removed, err)
						}
					case "recurring":
						job.Kind, job.Cron, job.FireAt, job.LastRun = capschedule.KindCron, "* * * * *", time.Time{}, start
					}
					add(job)
				}
				if d <= 0 || d > 30*time.Second {
					t.Fatalf("sleep = %s, want positive and <= 30s", d)
				}
				if sleeps > 8 {
					t.Fatal("dynamic job did not dispatch within four minutes")
				}
				return clock.Sleep(ctx, d)
			})
			err := rt.Start(ctx, func(_ context.Context, event agentkit.MessageEvent) error {
				dispatched++
				wantID := "early"
				if scenario == "update" {
					wantID = "far"
				}
				meta := event.Envelope.Metadata["schedule"].(map[string]any)
				if meta["jobId"] != wantID {
					t.Fatalf("dispatched job = %v, want %s", meta["jobId"], wantID)
				}
				wantTime := start.Add(time.Duration(dispatched) * time.Minute)
				if !clock.Now().Equal(wantTime) {
					t.Fatalf("dispatch at %s, want %s", clock.Now(), wantTime)
				}
				if scenario != "recurring" || dispatched == 2 {
					cancel()
				}
				return nil
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Start = %v", err)
			}
			jobs, err := registry.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, job := range jobs {
				if scenario == "remove" && job.ID == "far" {
					t.Fatal("removed job remains")
				}
				if job.ID != "early" && !(scenario == "update" && job.ID == "far") {
					continue
				}
				if scenario == "recurring" {
					if dispatched != 2 || !job.LastRun.Equal(start.Add(2*time.Minute)) || job.Fired {
						t.Fatalf("recurring state = %+v, dispatches = %d", job, dispatched)
					}
				} else if !job.Fired {
					t.Fatalf("one-shot was not marked fired: %+v", job)
				}
			}
		})
	}
}

func TestCronWakesBeforePollIntervalForKnownJob(t *testing.T) {
	rt, registry, clock := newCronRuntime(t, pluginschedule.CronConfig{PollSeconds: 30})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	due := clock.Now().Add(5 * time.Second)
	if _, err := registry.Add(ctx, capschedule.Job{ID: "soon", Kind: capschedule.KindAt, FireAt: due, Prompt: "soon"}); err != nil {
		t.Fatal(err)
	}
	sleeps, dispatched := 0, 0
	cronImpl(t, rt).SetClockForTest(clock.Now, func(ctx context.Context, d time.Duration) error {
		sleeps++
		if sleeps != 1 || d != 5*time.Second {
			t.Fatalf("sleep #%d = %s, want one 5s wait", sleeps, d)
		}
		return clock.Sleep(ctx, d)
	})
	err := rt.Start(ctx, func(_ context.Context, _ agentkit.MessageEvent) error {
		dispatched++
		if !clock.Now().Equal(due) {
			t.Fatalf("dispatch at %s, want %s", clock.Now(), due)
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || dispatched != 1 {
		t.Fatalf("Start = %v, dispatches = %d", err, dispatched)
	}
}
