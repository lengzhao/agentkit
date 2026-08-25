package schedule_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/schedule"
)

func newRegistry(t *testing.T) (capschedule.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := schedule.NewFile(schedule.FileConfig{Path: "schedule.json"}, schedule.FileDeps{
		Workspace: workspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg, filepath.Join(dir, "schedule.json")
}

func TestAddValidatesAndPersists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg, path := newRegistry(t)

	if _, err := reg.Add(ctx, capschedule.Job{Cron: "not a cron", Prompt: "x"}); err == nil {
		t.Fatal("expected a cron validation error")
	}
	if _, err := reg.Add(ctx, capschedule.Job{Cron: "@daily"}); err == nil {
		t.Fatal("expected an error for a job without a prompt")
	}

	job, err := reg.Add(ctx, capschedule.Job{Cron: "0 9 * * *", Prompt: "morning check"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if job.ID == "" {
		t.Fatal("Add should assign an id")
	}
	if job.Source != capschedule.SourceAgent {
		t.Fatalf("source = %q, want %q", job.Source, capschedule.SourceAgent)
	}
	// A new job must be anchored at creation, otherwise its first Next() is in
	// the past and it fires immediately instead of at 09:00.
	if job.LastRun.IsZero() {
		t.Fatal("Add should anchor LastRun")
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("registry file not written: %v", err)
	}

	// A fresh registry over the same file sees the job: this is what makes an
	// agent-created schedule survive a restart.
	reopened, err := schedule.NewFile(schedule.FileConfig{Path: "schedule.json"}, schedule.FileDeps{
		Workspace: workspace.Static(filepath.Dir(path)),
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := reopened.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Prompt != "morning check" {
		t.Fatalf("reopened registry = %+v", jobs)
	}
}

func TestRemoveReportsWhetherItExisted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg, _ := newRegistry(t)
	job, err := reg.Add(ctx, capschedule.Job{Cron: "@hourly", Prompt: "poll"})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := reg.Remove(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("Remove should report true for an existing job")
	}
	removed, err = reg.Remove(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("Remove should report false the second time")
	}
}

// TestSyncSourceLeavesOtherSourcesAlone is the whole point of tracking a source:
// editing the preset must not delete what the agent scheduled for itself.
func TestSyncSourceLeavesOtherSourcesAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg, _ := newRegistry(t)

	agentJob, err := reg.Add(ctx, capschedule.Job{Cron: "@daily", Prompt: "agent's own follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SyncSource(ctx, capschedule.SourceConfig, []capschedule.Job{
		{ID: "task-1", Cron: "0 9 * * *", Prompt: "from config"},
		{ID: "task-2", Cron: "0 18 * * *", Prompt: "also from config"},
	}); err != nil {
		t.Fatal(err)
	}

	// Now the preset drops task-2.
	if err := reg.SyncSource(ctx, capschedule.SourceConfig, []capschedule.Job{
		{ID: "task-1", Cron: "0 9 * * *", Prompt: "from config"},
	}); err != nil {
		t.Fatal(err)
	}

	jobs, err := reg.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]capschedule.Job{}
	for _, job := range jobs {
		byID[job.ID] = job
	}
	if _, ok := byID["task-2"]; ok {
		t.Fatal("a config job removed from the preset should be dropped")
	}
	if _, ok := byID["task-1"]; !ok {
		t.Fatal("task-1 should survive")
	}
	if _, ok := byID[agentJob.ID]; !ok {
		t.Fatalf("agent job %q was destroyed by a config sync: %+v", agentJob.ID, jobs)
	}
}

// TestSyncSourcePreservesAnchor stops a restart loop from delaying schedules: if
// SyncSource re-anchored LastRun to now, a daemon restarting every few minutes
// would push "0 9 * * *" forward forever.
func TestSyncSourcePreservesAnchor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg, _ := newRegistry(t)
	config := []capschedule.Job{{ID: "task-1", Cron: "0 9 * * *", Prompt: "morning"}}

	if err := reg.SyncSource(ctx, capschedule.SourceConfig, config); err != nil {
		t.Fatal(err)
	}
	first, err := reg.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	anchor := first[0].LastRun

	time.Sleep(5 * time.Millisecond)
	if err := reg.SyncSource(ctx, capschedule.SourceConfig, config); err != nil {
		t.Fatal(err)
	}
	second, err := reg.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !second[0].LastRun.Equal(anchor) {
		t.Fatalf("anchor moved from %s to %s across sync", anchor, second[0].LastRun)
	}
}

func TestDueFiresOnceAndSkipsMissedBoundaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg, _ := newRegistry(t)

	anchor := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)
	if err := reg.SyncSource(ctx, capschedule.SourceConfig, []capschedule.Job{
		{ID: "hourly", Cron: "0 * * * *", Prompt: "poll", LastRun: anchor},
	}); err != nil {
		t.Fatal(err)
	}

	// Before the boundary: nothing due.
	due, err := reg.Due(ctx, anchor.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("due before the boundary = %+v", due)
	}

	// Four hours later, four boundaries passed, but a backlog is not replayed:
	// exactly one fire, same as the timer platform's skip policy.
	due, err = reg.Due(ctx, anchor.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != "hourly" {
		t.Fatalf("due = %+v, want one hourly job", due)
	}

	// Immediately after, it is no longer due: Due stamped it.
	due, err = reg.Due(ctx, anchor.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("job fired twice for one boundary: %+v", due)
	}
}

func TestDueSkipsDisabledJobs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg, _ := newRegistry(t)
	anchor := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)
	if err := reg.SyncSource(ctx, capschedule.SourceConfig, []capschedule.Job{
		{ID: "on", Cron: "* * * * *", Prompt: "runs", LastRun: anchor},
		{ID: "off", Cron: "* * * * *", Prompt: "paused", LastRun: anchor, Disabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	due, err := reg.Due(ctx, anchor.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != "on" {
		t.Fatalf("due = %+v, want only the enabled job", due)
	}
}

func TestListSurvivesAMissingOrEmptyFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reg, path := newRegistry(t)

	// Missing file: an empty schedule, not an error.
	jobs, err := reg.List(ctx)
	if err != nil {
		t.Fatalf("List on a missing file: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v, want none", jobs)
	}

	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.List(ctx); err != nil {
		t.Fatalf("List on an empty file: %v", err)
	}

	// Corrupt content must be reported, not silently treated as empty: silently
	// dropping every schedule would be far worse than failing loudly.
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.List(ctx); err == nil {
		t.Fatal("expected an error for a corrupt schedule file")
	}
}
