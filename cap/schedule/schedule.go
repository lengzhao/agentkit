// Package schedule defines the calendar-scheduling capability: a durable set of
// cron jobs that schedule/cron fires and a tool can edit.
package schedule

import (
	"context"
	"errors"
	"time"
)

// ErrJobNotFound is returned when a registry operation targets a missing job id.
var ErrJobNotFound = errors.New("schedule: job not found")

// InFlightTimeout is how long a one-shot job may stay claimed before Due reclaims it.
const InFlightTimeout = 30 * time.Minute

// Job sources. Config jobs are owned by the preset and re-synced on every start;
// agent jobs are created at runtime and survive restarts untouched.
const (
	SourceConfig = "config"
	SourceAgent  = "agent"
)

// Job kinds distinguish repeating cron jobs from one-shot jobs.
const (
	KindCron  = "cron"
	KindDelay = "delay"
	KindAt    = "at"
)

// Job is one scheduled task.
type Job struct {
	ID     string `json:"id"`
	Kind   string `json:"kind,omitempty"`
	Cron   string `json:"cron,omitempty"`
	In     string `json:"in,omitempty"`
	FireAt time.Time `json:"fireAt,omitzero"`
	Prompt string    `json:"prompt,omitempty"`
	// Script is a workspace-relative bash script. When set, the job runs the
	// script directly instead of starting an agent turn.
	Script string `json:"script,omitempty"`
	Source string `json:"source"`
	// Disabled jobs stay in the registry but never fire.
	Disabled bool `json:"disabled,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// LastRun anchors the schedule. A new job is stamped at creation time so its
	// first fire is the next real boundary rather than immediately.
	LastRun time.Time `json:"lastRun,omitzero"`
	Fired   bool      `json:"fired,omitempty"`
	FiredAt time.Time `json:"firedAt,omitzero"`
	// InFlight marks a one-shot job claimed by Due but not yet MarkFired.
	InFlight   bool      `json:"inFlight,omitempty"`
	InFlightAt time.Time `json:"inFlightAt,omitzero"`
	LastError  string    `json:"lastError,omitempty"`
	// Note is free-form context the agent can leave for its future self.
	Note string `json:"note,omitempty"`
	// DeliverySessionID is the platform inbox to route outbound messages (e.g. send)
	// when the job fires. Captured automatically when tool/schedule creates the job.
	DeliverySessionID string `json:"deliverySessionId,omitempty"`
	PlatformID        string `json:"platformId,omitempty"`
	UserID            string `json:"userId,omitempty"`
	AgentID           string `json:"agentId,omitempty"`
	ChannelKey        string `json:"channelKey,omitempty"`
}

// Registry is the durable job set. Implementations must be safe for concurrent
// use: the firing runtime and the agent's tool touch it from different
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
	// MarkFired records that a one-shot job has been handled while retaining it
	// for audit/listing.
	MarkFired(ctx context.Context, id string, firedAt time.Time, fireErr error) error
}
