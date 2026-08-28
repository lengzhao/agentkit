package schedule

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/shell"
	"github.com/lengzhao/agentkit/cap/workspace"
)

const cronPlatformID = "schedule"

// CronJobSpec is one config-declared cron job. Prompt and script are mutually
// exclusive; script runs without an agent turn.
type CronJobSpec struct {
	ID     string `json:"id"`
	Cron   string `json:"cron"`
	Prompt string `json:"prompt,omitempty"`
	Script string `json:"script,omitempty"`
	Note   string `json:"note,omitempty"`
}

type CronConfig struct {
	// Jobs are reconciled into the registry on every start with source=config.
	Jobs []CronJobSpec `json:"jobs"`
	// SessionMode is fresh (default) or fixed.
	SessionMode string `json:"sessionMode"`
	// SessionID is the id prefix used for inbound turns.
	SessionID string `json:"sessionId"`
	// PollSeconds bounds how long before a job the agent adds mid-run is noticed.
	// Defaults to 30.
	PollSeconds int `json:"pollSeconds"`
}

type CronDeps struct {
	Schedule  capschedule.Registry `json:"schedule"`
	Workspace workspace.Service    `json:"workspace,omitempty"`
	Shell     shell.Executor       `json:"shell,omitempty"`
}

const defaultPollSeconds = 30

// Cron watches a shared registry and submits due jobs as inbound turns.
type Cron struct {
	registry  capschedule.Registry
	workspace workspace.Service
	shell     shell.Executor
	jobs      []capschedule.Job
	poll      time.Duration
	naming    sessionNamer
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error

	mu       sync.Mutex
	runCount int
	dueQueue []capschedule.Job
}

// NewCron registers schedule/cron: Resident calendar scheduler over a shared job registry.
//
// Best practices:
//   - Point tool/schedule at the same registry instance, or agent jobs never fire.
//   - Script jobs need workspace and shell deps; prompt jobs only need the registry.
//   - Missed boundaries are skipped, not backfilled.
func NewCron(cfg CronConfig, deps CronDeps) (capschedule.Runtime, error) {
	if deps.Schedule == nil {
		return nil, fmt.Errorf("schedule/cron requires schedule dependency")
	}
	jobs, err := parseCronJobs(cfg.Jobs)
	if err != nil {
		return nil, err
	}
	if err := validateScriptDeps(jobs, deps); err != nil {
		return nil, err
	}
	mode, err := resolveSessionMode(cfg.SessionMode)
	if err != nil {
		return nil, fmt.Errorf("schedule/cron: %w", err)
	}
	sessionID := strings.TrimSpace(cfg.SessionID)
	if sessionID == "" {
		sessionID = "schedule"
	}
	poll := time.Duration(cfg.PollSeconds) * time.Second
	if cfg.PollSeconds <= 0 {
		poll = defaultPollSeconds * time.Second
	}
	return &Cron{
		registry:  deps.Schedule,
		workspace: deps.Workspace,
		shell:     deps.Shell,
		jobs:      jobs,
		poll:      poll,
		naming:    newSessionNamer(mode, sessionID, nil),
		now:       time.Now,
		sleep:     sleepContext,
	}, nil
}

func parseCronJobs(specs []CronJobSpec) ([]capschedule.Job, error) {
	jobs := make([]capschedule.Job, 0, len(specs))
	for i, spec := range specs {
		spec.Prompt = strings.TrimSpace(spec.Prompt)
		spec.Script = strings.TrimSpace(spec.Script)
		spec.Cron = strings.TrimSpace(spec.Cron)
		if spec.Cron == "" {
			return nil, fmt.Errorf("schedule/cron job %d requires cron", i+1)
		}
		if spec.Prompt == "" && spec.Script == "" {
			return nil, fmt.Errorf("schedule/cron job %d requires prompt or script", i+1)
		}
		if spec.Prompt != "" && spec.Script != "" {
			return nil, fmt.Errorf("schedule/cron job %d: prompt and script are mutually exclusive", i+1)
		}
		if _, err := capschedule.ParseCron(spec.Cron); err != nil {
			return nil, fmt.Errorf("schedule/cron job %d: %w", i+1, err)
		}
		job := capschedule.Job{
			ID:     spec.ID,
			Cron:   spec.Cron,
			Note:   spec.Note,
			Source: capschedule.SourceConfig,
		}
		if job.ID == "" {
			job.ID = fmt.Sprintf("job-%d", i+1)
		}
		if spec.Script != "" {
			job.Script = spec.Script
		} else {
			job.Prompt = spec.Prompt
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func validateScriptDeps(jobs []capschedule.Job, deps CronDeps) error {
	needsScript := false
	for _, job := range jobs {
		if strings.TrimSpace(job.Script) != "" {
			needsScript = true
			break
		}
	}
	if !needsScript {
		return nil
	}
	if deps.Workspace == nil {
		return fmt.Errorf("schedule/cron jobs with script require workspace dependency")
	}
	if deps.Shell == nil {
		return fmt.Errorf("schedule/cron jobs with script require shell dependency")
	}
	return nil
}

func (c *Cron) Start(ctx context.Context, submit capschedule.SubmitFunc) error {
	if err := c.registry.SyncSource(ctx, capschedule.SourceConfig, c.jobs); err != nil {
		return fmt.Errorf("schedule/cron: sync jobs: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if job, ok := c.popDue(); ok {
			if err := c.fire(ctx, submit, job); err != nil {
				return err
			}
			continue
		}

		now := c.now()
		due, err := c.registry.Due(ctx, now)
		if err != nil {
			return err
		}
		if len(due) > 0 {
			c.pushDue(due)
			continue
		}
		if err := c.sleep(ctx, c.waitFor(ctx, now)); err != nil {
			return err
		}
	}
}

func (c *Cron) Stop(context.Context) error { return nil }

func (c *Cron) fire(ctx context.Context, submit capschedule.SubmitFunc, job capschedule.Job) error {
	if strings.TrimSpace(job.Script) != "" {
		return c.runScript(ctx, job.Script)
	}
	c.mu.Lock()
	run := c.runCount
	c.runCount++
	c.mu.Unlock()
	slog.Info("cron job firing", "job_id", job.ID, "cron", job.Cron, "source", job.Source)
	return submit(ctx, c.event(run, job.Prompt))
}

func (c *Cron) runScript(ctx context.Context, scriptPath string) error {
	path, err := c.workspace.Resolve(ctx, scriptPath)
	if err != nil {
		return fmt.Errorf("resolve script %q: %w", scriptPath, err)
	}
	result, err := c.shell.Run(ctx, shell.Request{Command: fmt.Sprintf("bash %q", path)})
	if err != nil {
		return fmt.Errorf("run script %q: %w", scriptPath, err)
	}
	slog.Info("cron script finished",
		"script", scriptPath,
		"exit_code", result.ExitCode,
		"stdout_bytes", len(result.Stdout),
		"stderr_bytes", len(result.Stderr),
	)
	if result.ExitCode != 0 {
		return fmt.Errorf("script %q failed with exit code %d: %s", scriptPath, result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return nil
}

func (c *Cron) waitFor(ctx context.Context, now time.Time) time.Duration {
	wait := c.poll
	jobs, err := c.registry.List(ctx)
	if err != nil {
		return wait
	}
	for _, job := range jobs {
		if job.Disabled {
			continue
		}
		next, ok := capschedule.NextFire(job, job.LastRun)
		if !ok {
			continue
		}
		if until := next.Sub(now); until > 0 && until < wait {
			wait = until
		}
	}
	if wait <= 0 {
		wait = time.Second
	}
	return wait
}

func (c *Cron) popDue() (capschedule.Job, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.dueQueue) == 0 {
		return capschedule.Job{}, false
	}
	job := c.dueQueue[0]
	c.dueQueue = c.dueQueue[1:]
	return job, true
}

func (c *Cron) pushDue(jobs []capschedule.Job) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dueQueue = append(c.dueQueue, jobs...)
}

func (c *Cron) event(run int, prompt string) agentkit.MessageEvent {
	return agentkit.MessageEvent{
		SessionID:  c.naming.forRun(run),
		PlatformID: cronPlatformID,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: prompt}},
		},
	}
}

// SetClockForTest replaces the clock and sleep so cron behaviour can be asserted
// without real waiting. Test-only.
func (c *Cron) SetClockForTest(now func() time.Time, sleep func(context.Context, time.Duration) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now != nil {
		c.now = now
	}
	if sleep != nil {
		c.sleep = sleep
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
