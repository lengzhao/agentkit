// Package schedule defines the calendar-scheduling capability: a durable set of
// cron jobs that a platform fires and a tool can edit. Splitting it this way is
// what lets the agent schedule its own follow-up work without knowing who runs
// it.
package schedule

import (
	"context"
	"time"
)

// Job sources. Config jobs are owned by the preset and re-synced on every start;
// agent jobs are created at runtime and survive restarts untouched.
const (
	SourceConfig = "config"
	SourceAgent  = "agent"
)

// Job is one scheduled task.
type Job struct {
	ID     string `json:"id"`
	Cron   string `json:"cron"`
	Prompt string `json:"prompt,omitempty"`
	// Script is a workspace-relative bash script. When set, the job runs the
	// script directly instead of starting an agent turn.
	Script string `json:"script,omitempty"`
	Source string `json:"source"`
	// Disabled jobs stay in the registry but never fire.
	Disabled bool `json:"disabled,omitempty"`
	// LastRun anchors the schedule. A new job is stamped at creation time so its
	// first fire is the next real boundary rather than immediately.
	LastRun time.Time `json:"lastRun,omitzero"`
	// Note is free-form context the agent can leave for its future self.
	Note string `json:"note,omitempty"`
}

// Registry is the durable job set. Implementations must be safe for concurrent
// use: the firing platform and the agent's tool touch it from different
// goroutines.
type Registry interface {
	List(ctx context.Context) ([]Job, error)
	// Add stores a job, assigning an ID when the given one is empty. It returns
	// the stored job.
	Add(ctx context.Context, job Job) (Job, error)
	// Remove deletes a job by ID, reporting whether it existed.
	Remove(ctx context.Context, id string) (bool, error)
	// SyncSource replaces every job with the given source, leaving other sources
	// alone. Used to reconcile config-declared jobs on startup.
	SyncSource(ctx context.Context, source string, jobs []Job) error
	// Due returns the enabled jobs whose next fire time has arrived, and stamps
	// them as run at now. Missed boundaries are skipped rather than backfilled.
	Due(ctx context.Context, now time.Time) ([]Job, error)
}

// NextFire reports when a job should next run, given its anchor. It is shared by
// registries and by anything that wants to show "next run" without firing.
func NextFire(job Job, after time.Time) (time.Time, bool) {
	sched, err := ParseCron(job.Cron)
	if err != nil {
		return time.Time{}, false
	}
	return sched.Next(after)
}
