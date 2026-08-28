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

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/shell"
	"github.com/lengzhao/agentkit/cap/workspace"
)

const workerPlatformID = "worker"

// TaskSpec is one worker task. In YAML it may be written either as a bare string
// (run once at startup) or as an object with prompt or script.
//
// Each task uses exactly one mode:
//   - prompt: send text to the agent as a turn
//   - script: run a workspace-relative bash script without an agent turn
//
// Calendar cron belongs in schedule/cron, not here.
type TaskSpec struct {
	Prompt string `json:"prompt"`
	// Script is a workspace-relative path to a bash script executed directly.
	Script string `json:"script,omitempty"`
	// Cron is rejected: use schedule/cron with the same registry instead.
	Cron string `json:"cron,omitempty"`
	// ID names the task in logs. Defaults to task-<n>.
	ID string `json:"id,omitempty"`
	// Note is free-form context stored with the job.
	Note string `json:"note,omitempty"`
}

// UnmarshalJSON accepts a bare string so `tasks: ["do a thing"]` keeps working
// alongside `tasks: [{prompt: "..."}]`.
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

type WorkerConfig struct {
	// Tasks each run as one turn at startup, in order. Positional command-line
	// arguments override this list.
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
}

type WorkerDeps struct {
	// Workspace resolves script paths. Required when any task uses script.
	Workspace workspace.Service `json:"workspace,omitempty"`
	// Shell runs script tasks. Required when any task uses script.
	Shell shell.Executor `json:"shell,omitempty"`
}

// Worker runs a batch of one-shot tasks and then reports EOF. It never reads
// stdin, so it is safe under systemd, cron, and CI.
type Worker struct {
	tasks     []TaskSpec
	workspace workspace.Service
	shell     shell.Executor
	naming    sessionNamer
	emitter   *emitter

	mu       sync.Mutex
	next     int
	runCount int
}

// NewWorker registers platform/worker: Headless one-shot task runner for prompts or shell scripts.
//
// Best practices:
//   - The worker exits at EOF after its task list; calendar cron belongs in schedule/cron.
//   - Script tasks need workspace and shell deps, checked at startup rather than silently skipped.
func NewWorker(cfg WorkerConfig, deps WorkerDeps) (agentkit.Platform, error) {
	tasks, err := resolveWorkerTasks(cfg)
	if err != nil {
		return nil, err
	}
	if err := validateScriptDeps(tasks, deps); err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
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

	return &Worker{
		tasks:     tasks,
		workspace: deps.Workspace,
		shell:     deps.Shell,
		naming:    newSessionNamer(mode, sessionID, nil),
		emitter:   newEmitter(cfg.Output, cfg.Stream),
	}, nil
}

func resolveWorkerTasks(cfg WorkerConfig) ([]TaskSpec, error) {
	if positional := positionalTask(); positional != "" {
		return []TaskSpec{{Prompt: positional}}, nil
	}
	tasks := make([]TaskSpec, 0, len(cfg.Tasks))
	for i, task := range cfg.Tasks {
		task.Prompt = strings.TrimSpace(task.Prompt)
		task.Script = strings.TrimSpace(task.Script)
		task.Cron = strings.TrimSpace(task.Cron)
		if task.Cron != "" {
			return nil, fmt.Errorf("platform/worker task %d has cron %q: use schedule/cron instead", i+1, task.Cron)
		}
		hasPrompt := task.Prompt != ""
		hasScript := task.Script != ""
		if hasPrompt && hasScript {
			return nil, fmt.Errorf("platform/worker task %d: prompt and script are mutually exclusive", i+1)
		}
		if !hasPrompt && !hasScript {
			continue
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

func validateScriptDeps(tasks []TaskSpec, deps WorkerDeps) error {
	needsScript := false
	for _, task := range tasks {
		if strings.TrimSpace(task.Script) != "" {
			needsScript = true
			break
		}
	}
	if !needsScript {
		return nil
	}
	if deps.Workspace == nil {
		return fmt.Errorf("platform/worker tasks with script require workspace dependency")
	}
	if deps.Shell == nil {
		return fmt.Errorf("platform/worker tasks with script require shell dependency")
	}
	return nil
}

func (w *Worker) PlatformID() string { return workerPlatformID }

func (w *Worker) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	if err := ctx.Err(); err != nil {
		return agentkit.MessageEvent{}, err
	}

	for {
		w.mu.Lock()
		if w.next >= len(w.tasks) {
			w.mu.Unlock()
			return agentkit.MessageEvent{}, io.EOF
		}
		task := w.tasks[w.next]
		w.next++
		run := w.runCount
		w.runCount++
		w.mu.Unlock()

		if task.Script != "" {
			if err := w.runScript(ctx, task.Script); err != nil {
				return agentkit.MessageEvent{}, err
			}
			continue
		}
		return w.event(run, task.Prompt), nil
	}
}

func (w *Worker) runScript(ctx context.Context, scriptPath string) error {
	path, err := w.workspace.Resolve(ctx, scriptPath)
	if err != nil {
		return fmt.Errorf("resolve script %q: %w", scriptPath, err)
	}
	result, err := w.shell.Run(ctx, shell.Request{Command: fmt.Sprintf("bash %q", path)})
	if err != nil {
		return fmt.Errorf("run script %q: %w", scriptPath, err)
	}
	slog.Info("task script finished",
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

func (w *Worker) event(run int, prompt string) agentkit.MessageEvent {
	return agentkit.MessageEvent{
		SessionID:  w.naming.forRun(run),
		PlatformID: workerPlatformID,
		Message:    userMessage(prompt),
	}
}

func (w *Worker) Send(_ context.Context, event agentkit.OutboundEvent) error {
	return w.emitter.send(event)
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
