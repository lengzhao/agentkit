package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/cap/filesystem"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
)

type FileConfig struct {
	// Path is JSON file holding the jobs, resolved through the workspace.
	Path string `json:"path"`
}

type FileDeps struct {
	FS     filesystem.Service `json:"fs"`
	Engine capschedule.Engine `json:"engine"`
}

// fileRegistry keeps jobs in a JSON file so an agent-created schedule survives a
// restart. The filesystem backend guarantees atomic replace (temp file + rename
// on local disk): a daemon killed mid-write must not come back to a truncated
// schedule. Concurrent readers and writers rely on atomic replace, not an
// in-process mutex.
type fileRegistry struct {
	relPath string
	fs      filesystem.Service
	engine  capschedule.Engine

	now func() time.Time
}

type fileState struct {
	Jobs []capschedule.Job `json:"jobs"`
}

// NewFile registers schedule/file: Durable cron job table, shared by schedule/cron and tool/capschedule.
//
// Best practices:
//   - Point schedule/cron and tool/schedule at one instance, or the agent will schedule jobs nothing fires.
//   - Jobs carry a source: config jobs are reconciled against the preset on every start, agent jobs are left alone.
//   - Writes go through a temp file and rename, so a process killed mid-write leaves no truncated table.
func NewFile(cfg FileConfig, deps FileDeps) (capschedule.Registry, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("schedule/file requires fs dependency")
	}
	if deps.Engine == nil {
		return nil, fmt.Errorf("schedule/file requires engine dependency")
	}
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		path = "capschedule.json"
	}
	return &fileRegistry{relPath: path, fs: deps.FS, engine: deps.Engine, now: time.Now}, nil
}

func (r *fileRegistry) List(ctx context.Context) ([]capschedule.Job, error) {
	state, err := r.loadState(ctx)
	if err != nil {
		return nil, err
	}
	return state.Jobs, nil
}

func (r *fileRegistry) Add(ctx context.Context, job capschedule.Job) (capschedule.Job, error) {
	if err := r.validateJob(job, false); err != nil {
		return capschedule.Job{}, err
	}
	if job.Source == "" {
		job.Source = capschedule.SourceAgent
	}
	job.Kind = job.NormalizedKind()

	state, err := r.loadState(ctx)
	if err != nil {
		return capschedule.Job{}, err
	}
	if job.ID == "" {
		job.ID = nextJobID(state.Jobs, job.Source)
	}
	if job.Kind == capschedule.KindCron && job.LastRun.IsZero() {
		// Anchor at creation time, otherwise the first Next() lands in the past
		// and the job fires immediately instead of at its capschedule.
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
	if err := r.saveState(ctx, state); err != nil {
		return capschedule.Job{}, err
	}
	return job, nil
}

func (r *fileRegistry) Remove(ctx context.Context, id string) (bool, error) {
	state, err := r.loadState(ctx)
	if err != nil {
		return false, err
	}
	kept := make([]capschedule.Job, 0, len(state.Jobs))
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
	if err := r.saveState(ctx, state); err != nil {
		return false, err
	}
	return true, nil
}

func (r *fileRegistry) validateJob(job capschedule.Job, allowScript bool) error {
	if strings.TrimSpace(job.Prompt) == "" && (!allowScript || strings.TrimSpace(job.Script) == "") {
		return fmt.Errorf("job requires a prompt")
	}
	switch job.NormalizedKind() {
	case capschedule.KindCron:
		if _, err := r.engine.ParseCron(job.Cron); err != nil {
			return err
		}
	case capschedule.KindDelay, capschedule.KindAt:
		if job.FireAt.IsZero() {
			return fmt.Errorf("%s job requires fireAt", job.NormalizedKind())
		}
	default:
		return fmt.Errorf("unknown schedule kind %q", job.Kind)
	}
	return nil
}

// SyncSource reconciles one source's jobs, preserving each surviving job's
// LastRun so a restart does not re-anchor (and therefore delay) the capschedule.
func (r *fileRegistry) SyncSource(ctx context.Context, source string, jobs []capschedule.Job) error {
	for i := range jobs {
		if jobs[i].Kind == "" {
			jobs[i].Kind = capschedule.KindCron
		}
		if err := r.validateJob(jobs[i], true); err != nil {
			return fmt.Errorf("job %q: %w", jobs[i].ID, err)
		}
		jobs[i].Source = source
	}

	state, err := r.loadState(ctx)
	if err != nil {
		return err
	}

	previous := make(map[string]capschedule.Job, len(state.Jobs))
	kept := make([]capschedule.Job, 0, len(state.Jobs)+len(jobs))
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
		} else if job.Kind == capschedule.KindCron && job.LastRun.IsZero() {
			job.LastRun = now
		}
		kept = append(kept, job)
	}
	state.Jobs = kept
	return r.saveState(ctx, state)
}

// Due returns the jobs whose next boundary has arrived and stamps them as run.
// Missed boundaries are skipped rather than backfilled, matching the timer
// platform: replaying a backlog of stale schedules is never what was meant.
func (r *fileRegistry) Due(ctx context.Context, now time.Time) ([]capschedule.Job, error) {
	state, err := r.loadState(ctx)
	if err != nil {
		return nil, err
	}

	var due []capschedule.Job
	changed := false
	for i, job := range state.Jobs {
		if job.Fired {
			continue
		}
		switch job.NormalizedKind() {
		case capschedule.KindDelay, capschedule.KindAt:
			if job.FireAt.After(now) {
				continue
			}
			if !job.InFlightAt.IsZero() && !job.InFlightExpired(now) {
				continue
			}
			state.Jobs[i].InFlightAt = now
			changed = true
			due = append(due, state.Jobs[i])
		default:
			next, ok := r.engine.NextFire(job, job.LastRun)
			if !ok || next.After(now) {
				continue
			}
			state.Jobs[i].LastRun = now
			changed = true
			due = append(due, state.Jobs[i])
		}
	}
	if changed {
		if err := r.saveState(ctx, state); err != nil {
			return nil, err
		}
	}
	// Stable order so a tick that fires several jobs is reproducible.
	sort.Slice(due, func(i, j int) bool { return due[i].ID < due[j].ID })
	return due, nil
}

func (r *fileRegistry) MarkFired(ctx context.Context, id string) error {
	state, err := r.loadState(ctx)
	if err != nil {
		return err
	}
	for i := range state.Jobs {
		if state.Jobs[i].ID != id {
			continue
		}
		state.Jobs[i].Fired = true
		state.Jobs[i].InFlightAt = time.Time{}
		return r.saveState(ctx, state)
	}
	return fmt.Errorf("%w: %q", capschedule.ErrJobNotFound, id)
}

func (r *fileRegistry) loadState(ctx context.Context) (fileState, error) {
	raw, err := r.fs.Read(ctx, r.relPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileState{}, nil
		}
		return fileState{}, err
	}
	if len(raw) == 0 {
		return fileState{}, nil
	}
	var state fileState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fileState{}, fmt.Errorf("parse schedule file %q: %w", r.relPath, err)
	}
	return state, nil
}

func (r *fileRegistry) saveState(ctx context.Context, state fileState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return r.fs.Write(ctx, r.relPath, raw)
}

// nextJobID picks the lowest unused sequence number for a source, so ids stay
// short and readable in logs.
func nextJobID(jobs []capschedule.Job, source string) string {
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
