package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type FileConfig struct {
	// Path is JSON file holding the jobs, resolved through the workspace.
	Path string `json:"path"`
}

type FileDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

// fileRegistry keeps jobs in a JSON file so an agent-created schedule survives a
// restart. Writes go through a temp file and rename: a daemon killed mid-write
// must not come back to a truncated schedule.
type fileRegistry struct {
	relPath   string
	workspace workspace.Service

	mu  sync.Mutex
	now func() time.Time
}

type fileState struct {
	Jobs []schedule.Job `json:"jobs"`
}

// NewFile registers schedule/file: Durable cron job table, shared by platform/worker and tool/schedule.
//
// Best practices:
//   - Point the worker and tool/schedule at one instance, or the agent will schedule jobs nothing fires.
//   - Jobs carry a source: config jobs are reconciled against the preset on every start, agent jobs are left alone.
//   - Writes go through a temp file and rename, so a process killed mid-write leaves no truncated table.
func NewFile(cfg FileConfig, deps FileDeps) (schedule.Registry, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("schedule/file requires workspace dependency")
	}
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		path = "schedule.json"
	}
	return &fileRegistry{relPath: path, workspace: deps.Workspace, now: time.Now}, nil
}

func (r *fileRegistry) List(ctx context.Context) ([]schedule.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.load(ctx)
	if err != nil {
		return nil, err
	}
	return state.Jobs, nil
}

func (r *fileRegistry) Add(ctx context.Context, job schedule.Job) (schedule.Job, error) {
	if _, err := schedule.ParseCron(job.Cron); err != nil {
		return schedule.Job{}, err
	}
	if strings.TrimSpace(job.Prompt) == "" {
		return schedule.Job{}, fmt.Errorf("job requires a prompt")
	}
	if job.Source == "" {
		job.Source = schedule.SourceAgent
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.load(ctx)
	if err != nil {
		return schedule.Job{}, err
	}
	if job.ID == "" {
		job.ID = nextJobID(state.Jobs, job.Source)
	}
	if job.LastRun.IsZero() {
		// Anchor at creation time, otherwise the first Next() lands in the past
		// and the job fires immediately instead of at its schedule.
		job.LastRun = r.now()
	}

	replaced := false
	for i, existing := range state.Jobs {
		if existing.ID == job.ID {
			state.Jobs[i] = job
			replaced = true
			break
		}
	}
	if !replaced {
		state.Jobs = append(state.Jobs, job)
	}
	if err := r.save(ctx, state); err != nil {
		return schedule.Job{}, err
	}
	return job, nil
}

func (r *fileRegistry) Remove(ctx context.Context, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.load(ctx)
	if err != nil {
		return false, err
	}
	kept := make([]schedule.Job, 0, len(state.Jobs))
	found := false
	for _, job := range state.Jobs {
		if job.ID == id {
			found = true
			continue
		}
		kept = append(kept, job)
	}
	if !found {
		return false, nil
	}
	state.Jobs = kept
	if err := r.save(ctx, state); err != nil {
		return false, err
	}
	return true, nil
}

// SyncSource reconciles one source's jobs, preserving each surviving job's
// LastRun so a restart does not re-anchor (and therefore delay) the schedule.
func (r *fileRegistry) SyncSource(ctx context.Context, source string, jobs []schedule.Job) error {
	for i := range jobs {
		if _, err := schedule.ParseCron(jobs[i].Cron); err != nil {
			return fmt.Errorf("job %q: %w", jobs[i].ID, err)
		}
		jobs[i].Source = source
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.load(ctx)
	if err != nil {
		return err
	}

	previous := make(map[string]schedule.Job, len(state.Jobs))
	kept := make([]schedule.Job, 0, len(state.Jobs)+len(jobs))
	for _, job := range state.Jobs {
		if job.Source == source {
			previous[job.ID] = job
			continue
		}
		kept = append(kept, job)
	}
	now := r.now()
	for _, job := range jobs {
		if old, ok := previous[job.ID]; ok && !old.LastRun.IsZero() {
			job.LastRun = old.LastRun
		} else if job.LastRun.IsZero() {
			job.LastRun = now
		}
		kept = append(kept, job)
	}
	state.Jobs = kept
	return r.save(ctx, state)
}

// Due returns the jobs whose next boundary has arrived and stamps them as run.
// Missed boundaries are skipped rather than backfilled, matching the timer
// platform: replaying a backlog of stale schedules is never what was meant.
func (r *fileRegistry) Due(ctx context.Context, now time.Time) ([]schedule.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.load(ctx)
	if err != nil {
		return nil, err
	}

	var due []schedule.Job
	changed := false
	for i, job := range state.Jobs {
		if job.Disabled {
			continue
		}
		next, ok := schedule.NextFire(job, job.LastRun)
		if !ok || next.After(now) {
			continue
		}
		state.Jobs[i].LastRun = now
		changed = true
		due = append(due, state.Jobs[i])
	}
	if changed {
		if err := r.save(ctx, state); err != nil {
			return nil, err
		}
	}
	// Stable order so a tick that fires several jobs is reproducible.
	sort.Slice(due, func(i, j int) bool { return due[i].ID < due[j].ID })
	return due, nil
}

func (r *fileRegistry) resolve(ctx context.Context) (string, error) {
	return r.workspace.Resolve(ctx, r.relPath)
}

func (r *fileRegistry) load(ctx context.Context) (fileState, error) {
	path, err := r.resolve(ctx)
	if err != nil {
		return fileState{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileState{}, nil
		}
		return fileState{}, err
	}
	if len(raw) == 0 {
		return fileState{}, nil
	}
	var state fileState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fileState{}, fmt.Errorf("parse schedule file %q: %w", path, err)
	}
	return state, nil
}

func (r *fileRegistry) save(ctx context.Context, state fileState) error {
	path, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

// nextJobID picks the lowest unused sequence number for a source, so ids stay
// short and readable in logs.
func nextJobID(jobs []schedule.Job, source string) string {
	used := make(map[string]bool, len(jobs))
	for _, job := range jobs {
		used[job.ID] = true
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d", source, i)
		if !used[candidate] {
			return candidate
		}
	}
}
