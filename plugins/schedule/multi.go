package schedule

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

const defaultGlobalSchedulePath = "global:schedules/schedule.json"

type MultiConfig struct {
	// Path is the shared schedule file, default global:schedules/schedule.json.
	Path string `json:"path"`
}

type MultiDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

// multiRegistry stores every job in one schedule file. List and Remove filter by
// the channel key derived from the current turn context.
type multiRegistry struct {
	inner schedule.Registry
}

// NewMulti registers schedule/multi: One shared schedule.json with per-channel list/remove filtering.
func NewMulti(cfg MultiConfig, deps MultiDeps) (schedule.Registry, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("schedule/multi requires workspace dependency")
	}
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		path = defaultGlobalSchedulePath
	}
	inner, err := NewFile(FileConfig{Path: path}, FileDeps{Workspace: deps.Workspace})
	if err != nil {
		return nil, err
	}
	return &multiRegistry{inner: inner}, nil
}

func (r *multiRegistry) List(ctx context.Context) ([]schedule.Job, error) {
	jobs, err := r.inner.List(ctx)
	if err != nil {
		return nil, err
	}
	key := session.ChannelKeyFromContext(ctx)
	if key == "" {
		return jobs, nil
	}
	filtered := make([]schedule.Job, 0, len(jobs))
	for _, job := range jobs {
		if session.ChannelKeyMatches(job.ChannelKey, key) {
			filtered = append(filtered, job)
		}
	}
	return filtered, nil
}

func (r *multiRegistry) Add(ctx context.Context, job schedule.Job) (schedule.Job, error) {
	key := strings.TrimSpace(job.ChannelKey)
	if key == "" {
		key = session.ChannelKeyFromContext(ctx)
	}
	if key == "" {
		return schedule.Job{}, fmt.Errorf("schedule add requires channel context")
	}
	job.ChannelKey = key
	return r.inner.Add(ctx, job)
}

func (r *multiRegistry) Remove(ctx context.Context, id string) (bool, error) {
	key := session.ChannelKeyFromContext(ctx)
	if key != "" {
		jobs, err := r.inner.List(ctx)
		if err != nil {
			return false, err
		}
		found := false
		for _, job := range jobs {
			if job.ID != id {
				continue
			}
			if !session.ChannelKeyMatches(job.ChannelKey, key) {
				return false, nil
			}
			found = true
			break
		}
		if !found {
			return false, nil
		}
	}
	return r.inner.Remove(ctx, id)
}

func (r *multiRegistry) SyncSource(ctx context.Context, source string, jobs []schedule.Job) error {
	return r.inner.SyncSource(ctx, source, jobs)
}

func (r *multiRegistry) Due(ctx context.Context, now time.Time) ([]schedule.Job, error) {
	return r.inner.Due(ctx, now)
}

func (r *multiRegistry) MarkFired(ctx context.Context, id string, firedAt time.Time, fireErr error) error {
	return r.inner.MarkFired(ctx, id, firedAt, fireErr)
}
