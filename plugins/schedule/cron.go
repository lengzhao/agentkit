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
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
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
	// SessionMode is stateless (default), reuse, or fixed.
	SessionMode string `json:"sessionMode"`
	// SessionID is the id prefix used for inbound turns.
	SessionID string `json:"sessionId"`
	// PollSeconds is the idle backoff when no jobs are scheduled.
	// Defaults to 30.
	PollSeconds int `json:"pollSeconds"`
	// MissedGraceSeconds is how long a missed one-shot job is still fired.
	// Older one-shots are marked stale instead of backfilled. Defaults to 300.
	MissedGraceSeconds int `json:"missedGraceSeconds"`
}

type CronDeps struct {
	Schedule  capschedule.Registry `json:"schedule"`
	Workspace workspace.Service    `json:"workspace,omitempty"`
	Shell     shell.Executor       `json:"shell,omitempty"`
}

const defaultPollSeconds = 30
const defaultMissedGraceSeconds = 300

// Cron watches a shared registry and submits due jobs as inbound turns.
type Cron struct {
	registry    capschedule.Registry
	workspace   workspace.Service
	shell       shell.Executor
	jobs        []capschedule.Job
	poll        time.Duration
	missedGrace time.Duration
	naming      sessionNamer
	sessionMode string
	now         func() time.Time
	wait        func(context.Context, time.Duration) error

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
	missedGrace := time.Duration(cfg.MissedGraceSeconds) * time.Second
	if cfg.MissedGraceSeconds <= 0 {
		missedGrace = defaultMissedGraceSeconds * time.Second
	}
	return &Cron{
		registry:    deps.Schedule,
		workspace:   deps.Workspace,
		shell:       deps.Shell,
		jobs:        jobs,
		poll:        poll,
		missedGrace: missedGrace,
		naming:      newSessionNamer(mode, sessionID, nil),
		sessionMode: mode,
		now:         time.Now,
		wait:        waitTimer,
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
			Kind:   capschedule.KindCron,
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
		if err := c.wait(ctx, c.nextWake(ctx, now)); err != nil {
			return err
		}
	}
}

func (c *Cron) Stop(context.Context) error { return nil }

func (c *Cron) fire(ctx context.Context, submit capschedule.SubmitFunc, job capschedule.Job) error {
	if strings.TrimSpace(job.Script) != "" {
		return c.runScript(ctx, job.Script)
	}
	now := c.now()
	if c.isStaleOneShot(job, now) {
		err := fmt.Errorf("missed by %v (stale)", now.Sub(job.FireAt))
		slog.Warn("cron one-shot skipped as stale", "job_id", job.ID, "fire_at", job.FireAt, "err", err)
		return c.registry.MarkFired(ctx, job.ID, now, err)
	}
	c.mu.Lock()
	run := c.runCount
	c.runCount++
	c.mu.Unlock()
	slog.Info("cron job firing", "job_id", job.ID, "kind", capschedule.JobKind(job), "cron", job.Cron, "source", job.Source)
	err := submit(ctx, c.event(run, job))
	if capschedule.IsOneShot(job) {
		if markErr := c.registry.MarkFired(ctx, job.ID, now, err); markErr != nil {
			return fmt.Errorf("mark one-shot job %q fired: %w", job.ID, markErr)
		}
	}
	if err != nil {
		return err
	}
	return nil
}

func (c *Cron) isStaleOneShot(job capschedule.Job, now time.Time) bool {
	return capschedule.IsOneShot(job) && !job.FireAt.IsZero() && now.Sub(job.FireAt) > c.missedGrace
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

func (c *Cron) nextWake(ctx context.Context, now time.Time) time.Duration {
	jobs, err := c.registry.List(ctx)
	if err != nil {
		return c.poll
	}
	var next time.Time
	found := false
	for _, job := range jobs {
		if job.Disabled || job.Fired {
			continue
		}
		fireAt, ok := capschedule.NextFire(job, job.LastRun)
		if !ok {
			continue
		}
		if !found || fireAt.Before(next) {
			next = fireAt
			found = true
		}
	}
	if !found {
		return c.poll
	}
	wait := next.Sub(now)
	if wait < time.Millisecond {
		return time.Millisecond
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

func (c *Cron) event(run int, job capschedule.Job) agentkit.MessageEvent {
	now := c.now()
	platformID := cronPlatformID
	deliverySessionID := agentkit.SessionID("")
	if delivery := strings.TrimSpace(job.DeliverySessionID); delivery != "" {
		deliverySessionID = agentkit.SessionID(delivery)
		platformID = strings.TrimSpace(job.PlatformID)
		if platformID == "" {
			platformID = session.ParseDelivery(deliverySessionID, job.UserID).Platform
		}
	}

	var sessionID agentkit.SessionID
	switch c.sessionMode {
	case capschedule.SessionModeReuse:
		if deliverySessionID != "" {
			sessionID = deliverySessionID
		} else {
			sessionID = c.naming.forRun(run)
		}
	case capschedule.SessionModeFixed:
		sessionID = c.naming.forRun(run)
	default:
		sessionID = statelessSessionID(job, now)
	}

	prompt := scheduleInboundPrompt(job)
	evt := agentkit.MessageEvent{
		PlatformID: platformID,
		UserID:     strings.TrimSpace(job.UserID),
		Envelope: agentkit.TurnEnvelope{
			Conversation: string(sessionID),
		},
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: prompt}},
		},
		Metadata: map[string]any{
			"schedule": map[string]any{
				"fired":       true,
				"jobId":       job.ID,
				"kind":        capschedule.JobKind(job),
				"sessionMode": c.sessionMode,
			},
		},
	}
	if deliverySessionID != "" {
		evt = common.WithDeliverySession(evt, platformID, deliverySessionID)
	}
	if agent := strings.TrimSpace(job.AgentID); agent != "" {
		evt.AgentID = agentkit.AgentID(agent)
	}
	return evt
}

func scheduleInboundPrompt(job capschedule.Job) string {
	kind := capschedule.JobKind(job)
	desc := strings.TrimSpace(job.Note)
	if desc == "" {
		desc = strings.TrimSpace(job.Prompt)
	}
	if len([]rune(desc)) > 40 {
		desc = string([]rune(desc)[:40]) + "…"
	}
	header := fmt.Sprintf("[schedule kind=%s id=%s]", kind, job.ID)
	if desc != "" {
		header += fmt.Sprintf(" ⏰ %s", desc)
	}
	return header + "\n\n" +
		"这是一次定时任务触发。请用 send 把提醒发给用户（只发一次）。\n\n" +
		strings.TrimSpace(job.Prompt)
}

// SetClockForTest replaces the clock and wait so cron behaviour can be asserted
// without real waiting. Test-only.
func (c *Cron) SetClockForTest(now func() time.Time, wait func(context.Context, time.Duration) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now != nil {
		c.now = now
	}
	if wait != nil {
		c.wait = wait
	}
}

func waitTimer(ctx context.Context, d time.Duration) error {
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
