package headless

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/schedule"
)

// TaskSpec is one worker task. In YAML it may be written either as a bare string
// (run once at startup) or as an object with a cron expression (run on that
// schedule, keeping the process resident).
type TaskSpec struct {
	Prompt string `json:"prompt"`
	// Cron, when set, turns this task into a scheduled job instead of a
	// run-once-at-startup task.
	Cron string `json:"cron,omitempty"`
	// ID names the job in the registry and in logs. Defaults to task-<n>.
	ID string `json:"id,omitempty"`
	// Note is free-form context stored with the job.
	Note string `json:"note,omitempty"`
}

// UnmarshalJSON accepts a bare string so `tasks: ["do a thing"]` keeps working
// alongside `tasks: [{prompt: "...", cron: "0 9 * * *"}]`.
func (t *TaskSpec) UnmarshalJSON(raw []byte) error {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, `"`) {
		var prompt string
		if err := json.Unmarshal(raw, &prompt); err != nil {
			return err
		}
		*t = TaskSpec{Prompt: prompt}
		return nil
	}
	type plain TaskSpec
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*t = TaskSpec(decoded)
	return nil
}

func (t TaskSpec) scheduled() bool { return strings.TrimSpace(t.Cron) != "" }

type WorkerConfig struct {
	// Tasks each run as one turn. Entries without a cron run once at startup, in
	// order; entries with a cron are registered as scheduled jobs. Positional
	// command-line arguments override this list.
	Tasks []TaskSpec `json:"tasks"`
	// Prompt is the single-task shorthand, used when Tasks is empty.
	Prompt string `json:"prompt"`
	// SessionMode is fresh (default) or fixed.
	SessionMode string `json:"sessionMode"`
	// SessionID is the id used in fixed mode, and the prefix in fresh mode.
	SessionID string `json:"sessionId"`
	// Output is text (default) or json, one event object per line.
	Output string `json:"output"`
	// Stream echoes assistant deltas as they arrive. Off by default: an
	// unattended run wants the result, not the typing.
	Stream bool `json:"stream"`
	// PollSeconds is how often the cron loop re-reads the registry, which is what
	// bounds the delay before a job the agent just scheduled is noticed.
	// Defaults to 30.
	PollSeconds int `json:"pollSeconds"`
}

type WorkerDeps struct {
	// Schedule enables cron mode. Without it, cron-bearing tasks are a config
	// error rather than a silently ignored setting.
	Schedule schedule.Registry `json:"schedule,omitempty"`
}

const defaultPollSeconds = 30

// Worker runs a batch of tasks and, when a schedule registry is wired in, then
// stays resident firing cron jobs. It never reads stdin, so it is safe under
// cron, systemd, and CI.
type Worker struct {
	immediate []string
	registry  schedule.Registry
	poll      time.Duration
	naming    sessionNamer
	emitter   *emitter
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error

	mu       sync.Mutex
	next     int
	runCount int
	// dueQueue holds jobs already fetched from the registry but not yet handed to
	// the runner, so one tick firing several jobs delivers them one at a time.
	dueQueue []schedule.Job
}

func NewWorker(cfg WorkerConfig, deps WorkerDeps) (agentkit.Platform, error) {
	tasks, err := resolveWorkerTasks(cfg)
	if err != nil {
		return nil, err
	}
	scheduled, immediate := splitTasks(tasks)
	if len(scheduled) > 0 && deps.Schedule == nil {
		return nil, fmt.Errorf("platform/worker has %d cron task(s) but no schedule dependency", len(scheduled))
	}
	if len(immediate) == 0 && len(scheduled) == 0 {
		return nil, fmt.Errorf("platform/worker requires a positional argument, tasks, or prompt")
	}

	mode, err := resolveSessionMode(cfg.SessionMode)
	if err != nil {
		return nil, fmt.Errorf("platform/worker: %w", err)
	}
	sessionID := strings.TrimSpace(cfg.SessionID)
	if sessionID == "" {
		sessionID = "worker"
	}
	poll := time.Duration(cfg.PollSeconds) * time.Second
	if cfg.PollSeconds <= 0 {
		poll = defaultPollSeconds * time.Second
	}

	w := &Worker{
		immediate: immediate,
		registry:  deps.Schedule,
		poll:      poll,
		naming:    newSessionNamer(mode, sessionID, nil),
		emitter:   newEmitter(cfg.Output, cfg.Stream),
		now:       time.Now,
		sleep:     sleepContext,
	}

	// Reconcile config-declared jobs up front so removing one from the preset
	// also removes it from the durable registry, while agent-created jobs stay.
	if deps.Schedule != nil {
		if err := deps.Schedule.SyncSource(context.Background(), schedule.SourceConfig, scheduled); err != nil {
			return nil, fmt.Errorf("platform/worker: sync scheduled tasks: %w", err)
		}
	}
	return w, nil
}

// resolveWorkerTasks applies the precedence: a task named on the command line
// beats the config, so a preset can ship a default without shadowing it.
func resolveWorkerTasks(cfg WorkerConfig) ([]TaskSpec, error) {
	if positional := positionalTask(); positional != "" {
		return []TaskSpec{{Prompt: positional}}, nil
	}
	tasks := make([]TaskSpec, 0, len(cfg.Tasks))
	for i, task := range cfg.Tasks {
		task.Prompt = strings.TrimSpace(task.Prompt)
		task.Cron = strings.TrimSpace(task.Cron)
		if task.Prompt == "" {
			if task.Cron == "" {
				continue // a blank entry is just noise
			}
			return nil, fmt.Errorf("platform/worker task %d has a cron but no prompt", i+1)
		}
		if task.ID == "" {
			task.ID = fmt.Sprintf("task-%d", i+1)
		}
		tasks = append(tasks, task)
	}
	if len(tasks) == 0 && strings.TrimSpace(cfg.Prompt) != "" {
		tasks = append(tasks, TaskSpec{ID: "task-1", Prompt: strings.TrimSpace(cfg.Prompt)})
	}
	return tasks, nil
}

func splitTasks(tasks []TaskSpec) (scheduled []schedule.Job, immediate []string) {
	for _, task := range tasks {
		if !task.scheduled() {
			immediate = append(immediate, task.Prompt)
			continue
		}
		scheduled = append(scheduled, schedule.Job{
			ID:     task.ID,
			Cron:   task.Cron,
			Prompt: task.Prompt,
			Note:   task.Note,
			Source: schedule.SourceConfig,
		})
	}
	return scheduled, immediate
}

func (w *Worker) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	if err := ctx.Err(); err != nil {
		return agentkit.MessageEvent{}, err
	}

	// Startup tasks first: a resident worker should do its immediate work before
	// settling into the schedule.
	w.mu.Lock()
	if w.next < len(w.immediate) {
		prompt := w.immediate[w.next]
		w.next++
		run := w.runCount
		w.runCount++
		w.mu.Unlock()
		return w.event(run, prompt), nil
	}
	w.mu.Unlock()

	if w.registry == nil {
		return agentkit.MessageEvent{}, io.EOF
	}
	return w.receiveScheduled(ctx)
}

// receiveScheduled waits for the next due job. It polls rather than sleeping
// straight to the next boundary so a job the agent adds mid-run is noticed within
// one poll interval instead of after the current sleep.
func (w *Worker) receiveScheduled(ctx context.Context) (agentkit.MessageEvent, error) {
	for {
		if job, ok := w.popDue(); ok {
			w.mu.Lock()
			run := w.runCount
			w.runCount++
			w.mu.Unlock()
			slog.Info("cron job firing", "job_id", job.ID, "cron", job.Cron, "source", job.Source)
			return w.event(run, job.Prompt), nil
		}

		now := w.now()
		due, err := w.registry.Due(ctx, now)
		if err != nil {
			return agentkit.MessageEvent{}, err
		}
		if len(due) > 0 {
			w.pushDue(due)
			continue
		}
		if err := w.sleep(ctx, w.waitFor(ctx, now)); err != nil {
			return agentkit.MessageEvent{}, err
		}
	}
}

// waitFor is the shorter of "until the next boundary" and the poll interval.
func (w *Worker) waitFor(ctx context.Context, now time.Time) time.Duration {
	wait := w.poll
	jobs, err := w.registry.List(ctx)
	if err != nil {
		return wait
	}
	for _, job := range jobs {
		if job.Disabled {
			continue
		}
		next, ok := schedule.NextFire(job, job.LastRun)
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

func (w *Worker) popDue() (schedule.Job, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.dueQueue) == 0 {
		return schedule.Job{}, false
	}
	job := w.dueQueue[0]
	w.dueQueue = w.dueQueue[1:]
	return job, true
}

func (w *Worker) pushDue(jobs []schedule.Job) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dueQueue = append(w.dueQueue, jobs...)
}

func (w *Worker) event(run int, prompt string) agentkit.MessageEvent {
	return agentkit.MessageEvent{
		SessionID:  w.naming.forRun(run),
		PlatformID: "worker",
		Message:    userMessage(prompt),
	}
}

func (w *Worker) Send(_ context.Context, event agentkit.OutboundEvent) error {
	return w.emitter.send(event)
}

// SetClockForTest replaces the clock and sleep so cron behaviour can be asserted
// without real waiting. Test-only.
func (w *Worker) SetClockForTest(now func() time.Time, sleep func(context.Context, time.Duration) error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if now != nil {
		w.now = now
	}
	if sleep != nil {
		w.sleep = sleep
	}
}

// positionalTask reads the command line tail so `agent -config worker.yaml "do
// it"` works without editing the config.
func positionalTask() string {
	var args []string
	if flag.Parsed() {
		args = flag.Args()
	} else {
		args = os.Args[1:]
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return ""
	}
	return strings.TrimSpace(strings.Join(args, " "))
}
