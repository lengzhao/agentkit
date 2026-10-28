package schedule

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/shell"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

const cronPlatformID = "schedule"

// CronJobSpec is one config-declared cron job. Prompt and script are mutually
// exclusive; script runs without an agent turn.
//
// Config jobs used to carry no destination, so a fired job degraded to the
// synthetic "schedule" platform and its result could never be delivered. Give
// the spec a delivery target (an id.list session id) and optionally an owner
// and agent, and the fire routes exactly like an agent-created job.
type CronJobSpec struct {
	ID     string `json:"id"`
	Cron   string `json:"cron"`
	Prompt string `json:"prompt,omitempty"`
	Script string `json:"script,omitempty"`
	Note   string `json:"note,omitempty"`
	// DeliverTo is the destination session id, e.g.
	// "lark:oc_xxx:u:ou_yyy" or "chat-api:default:t:conv_1". When set, the
	// fired turn delivers its answer there; when empty the turn still runs but
	// has no user-facing destination (metadata-only jobs).
	DeliverTo string `json:"deliverTo,omitempty"`
	// Platform overrides the delivery protocol; defaults to the protocol
	// parsed from DeliverTo.
	Platform string `json:"platform,omitempty"`
	// Owner opens the scope: only Owner answers the permission prompts of a
	// job that routes back to an interactive platform.
	Owner string `json:"owner,omitempty"`
	// AgentID pins the fire to one agent; empty uses the loop default.
	AgentID string `json:"agentId,omitempty"`
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
	// PromptTemplate replaces the default preamble prepended to every fired
	// prompt. Use it to name a host-specific tool; leave it empty and the
	// default mentions no tool at all.
	PromptTemplate string `json:"promptTemplate"`
}

type CronDeps struct {
	Schedule  capschedule.Registry `json:"schedule"`
	Workspace workspace.Service    `json:"workspace,omitempty"`
	Shell     shell.Executor       `json:"shell,omitempty"`
	Engine    capschedule.Engine   `json:"engine"`
}

const defaultPollSeconds = 30
const defaultMissedGraceSeconds = 300

// SetDefaults implements pluginkit.Defaulter.
func (c *CronConfig) SetDefaults() {
	if c.PollSeconds == 0 {
		c.PollSeconds = defaultPollSeconds
	}
	if c.MissedGraceSeconds == 0 {
		c.MissedGraceSeconds = defaultMissedGraceSeconds
	}
	if strings.TrimSpace(c.SessionID) == "" {
		c.SessionID = "schedule"
	}
}

// Validate implements pluginkit.Validator.
func (c *CronConfig) Validate() error {
	if c.PollSeconds <= 0 {
		return fmt.Errorf("schedule/cron pollSeconds must be positive")
	}
	if c.MissedGraceSeconds <= 0 {
		return fmt.Errorf("schedule/cron missedGraceSeconds must be positive")
	}
	if _, err := resolveSessionMode(c.SessionMode); err != nil {
		return fmt.Errorf("schedule/cron: %w", err)
	}
	for i, spec := range c.Jobs {
		if strings.TrimSpace(spec.Cron) == "" {
			return fmt.Errorf("schedule/cron job %d requires cron", i+1)
		}
		prompt, script := strings.TrimSpace(spec.Prompt), strings.TrimSpace(spec.Script)
		if prompt == "" && script == "" {
			return fmt.Errorf("schedule/cron job %d requires prompt or script", i+1)
		}
		if prompt != "" && script != "" {
			return fmt.Errorf("schedule/cron job %d: prompt and script are mutually exclusive", i+1)
		}
	}
	return nil
}

// Cron watches a shared registry and submits due jobs as inbound turns.
type Cron struct {
	registry       capschedule.Registry
	workspace      workspace.Service
	shell          shell.Executor
	engine         capschedule.Engine
	jobs           []capschedule.Job
	poll           time.Duration
	missedGrace    time.Duration
	naming         sessionNamer
	sessionMode    string
	promptTemplate string
	now            func() time.Time
	wait           func(context.Context, time.Duration) error

	mu       sync.Mutex
	runCount int
	dueQueue []capschedule.Job
	loopHook atomic.Pointer[func()] // test-only via SetLoopHookForTest
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
	if deps.Engine == nil {
		return nil, fmt.Errorf("schedule/cron requires engine dependency")
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	jobs, err := parseCronJobs(cfg.Jobs, deps.Engine)
	if err != nil {
		return nil, err
	}
	if err := validateScriptDeps(jobs, deps); err != nil {
		return nil, err
	}
	mode, _ := resolveSessionMode(cfg.SessionMode) // Validate 已保证合法
	sessionID := strings.TrimSpace(cfg.SessionID)
	poll := time.Duration(cfg.PollSeconds) * time.Second
	missedGrace := time.Duration(cfg.MissedGraceSeconds) * time.Second
	return &Cron{
		registry:       deps.Schedule,
		workspace:      deps.Workspace,
		shell:          deps.Shell,
		engine:         deps.Engine,
		jobs:           jobs,
		poll:           poll,
		missedGrace:    missedGrace,
		naming:         newSessionNamer(mode, sessionID, nil),
		sessionMode:    mode,
		promptTemplate: strings.TrimSpace(cfg.PromptTemplate),
		now:            time.Now,
		wait:           waitTimer,
	}, nil
}

func parseCronJobs(specs []CronJobSpec, engine capschedule.Engine) ([]capschedule.Job, error) {
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
		if _, err := engine.ParseCron(spec.Cron); err != nil {
			return nil, fmt.Errorf("schedule/cron job %d: %w", i+1, err)
		}
		job := capschedule.Job{
			ID:      spec.ID,
			Kind:    capschedule.KindCron,
			Cron:    spec.Cron,
			Note:    spec.Note,
			Source:  capschedule.SourceConfig,
			AgentID: agentkit.AgentID(strings.TrimSpace(spec.AgentID)),
			Actor:   agentkit.ActorRef{UserID: strings.TrimSpace(spec.Owner)},
		}
		if route, err := configJobRoute(spec); err != nil {
			return nil, fmt.Errorf("schedule/cron job %d: %w", i+1, err)
		} else {
			job.Route = route
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

// configJobRoute turns a spec's DeliverTo/Platform into a stable delivery route.
// An empty DeliverTo yields a zero route: the job still fires as a side turn,
// it just has nowhere user-facing to answer.
func configJobRoute(spec CronJobSpec) (agentkit.RouteRef, error) {
	delivery := strings.TrimSpace(spec.DeliverTo)
	if delivery == "" {
		return agentkit.RouteRef{}, nil
	}
	platform := strings.TrimSpace(spec.Platform)
	if platform == "" {
		platform = rctx.ParseDelivery(agentkit.SessionID(delivery), strings.TrimSpace(spec.Owner)).Platform
	}
	if platform == "" {
		return agentkit.RouteRef{}, fmt.Errorf("deliverTo %q needs a platform (set platform or use a qualified session id)", delivery)
	}
	return rctx.SessionRouteFromDelivery(platform, agentkit.SessionID(delivery), "").Stable(), nil
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
	backoff := time.Second
	for {
		if hook := c.loopHook.Load(); hook != nil {
			(*hook)()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if job, ok := c.popDue(); ok {
			if err := c.fire(ctx, submit, job); err != nil {
				slog.Error("cron fire failed, scheduler continues", "job_id", job.ID, "err", err)
			}
			continue
		}

		now := c.now()
		due, err := c.registry.Due(ctx, now)
		if err != nil {
			slog.Error("cron due failed, scheduler retries", "retry_in", backoff, "err", err)
			if err := c.wait(ctx, backoff); err != nil {
				return err
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
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
		return c.registry.MarkFired(ctx, job.ID)
	}
	c.mu.Lock()
	run := c.runCount
	c.runCount++
	c.mu.Unlock()
	slog.Info("cron job firing", "job_id", job.ID, "kind", job.NormalizedKind(), "cron", job.Cron, "source", job.Source)
	err := submit(ctx, c.event(run, job))
	if job.IsOneShot() {
		if markErr := c.registry.MarkFired(ctx, job.ID); markErr != nil {
			return fmt.Errorf("mark one-shot job %q fired: %w", job.ID, markErr)
		}
	}
	if err != nil {
		return err
	}
	return nil
}

func (c *Cron) isStaleOneShot(job capschedule.Job, now time.Time) bool {
	return job.IsOneShot() && !job.FireAt.IsZero() && now.Sub(job.FireAt) > c.missedGrace
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
		if job.Fired {
			continue
		}
		fireAt, ok := c.engine.NextFire(job, job.LastRun)
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
	if delivery, ok := rctx.RouteSessionID(job.Route); ok && delivery != "" {
		deliverySessionID = delivery
		platformID = job.Route.DestinationID()
	}
	var sessionID agentkit.SessionID
	switch c.sessionMode {
	case capschedule.SessionModeReuse:
		if deliverySessionID != "" {
			sessionID = rctx.ActiveEntryKey(job.Route, rctx.RoutePolicy{ActiveEntryMode: rctx.ActiveEntryDelivery}, job.Actor.UserID)
		} else {
			sessionID = c.naming.forRun(run)
		}
	case capschedule.SessionModeFixed:
		sessionID = c.naming.forRun(run)
	default:
		sessionID = statelessSessionID(job, now)
	}

	route := job.Route.Stable()
	if deliverySessionID == "" {
		route = agentkit.RouteRef{Platform: cronPlatformID, Instance: platformID}
	}
	route.Instance = route.DestinationID()
	evt := agentkit.MessageEvent{
		Envelope: agentkit.TurnEnvelope{
			Route:        route,
			Conversation: string(sessionID),
			Actor:        job.Actor,
			AgentID:      job.AgentID,
			Metadata:     map[string]any{"schedule": map[string]any{"fired": true, "jobId": job.ID, "kind": job.NormalizedKind(), "sessionMode": c.sessionMode}},
		},
		Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: c.scheduleInboundPrompt(job)}}},
	}
	return evt
}

func (c *Cron) scheduleInboundPrompt(job capschedule.Job) string {
	kind := job.NormalizedKind()
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
	preamble := c.promptTemplate
	if preamble == "" {
		preamble = defaultPromptPreamble
	}
	return header + "\n\n" + preamble + "\n\n" + strings.TrimSpace(job.Prompt)
}

const defaultPromptPreamble = "这是一次定时任务触发，请完成下面的任务；结果会按计划投递给用户，不需要额外调用工具发送。"

// SetLoopHookForTest runs hook at the top of each Start loop iteration. Test-only.
func (c *Cron) SetLoopHookForTest(hook func()) {
	if hook == nil {
		c.loopHook.Store(nil)
		return
	}
	c.loopHook.Store(&hook)
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
