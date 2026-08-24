package headless

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
)

type WorkerConfig struct {
	// Tasks each run as one turn, in order. Used when the command line carries
	// no positional task.
	Tasks []string `json:"tasks"`
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

// Worker runs a fixed list of tasks and then reports EOF, which ends the
// process. It never reads stdin, so it is safe under cron, systemd, and CI.
type Worker struct {
	tasks   []string
	naming  sessionNamer
	emitter *emitter

	mu   sync.Mutex
	next int
}

func NewWorker(cfg WorkerConfig) (agentkit.Platform, error) {
	// A task named on the command line is the most specific intent, so it wins
	// over the config. That lets a preset ship a default task without shadowing
	// `agent -config worker.yaml "something else"`.
	tasks := normalizeTasks(positionalTask())
	if len(tasks) == 0 {
		tasks = normalizeTasks(cfg.Tasks)
	}
	if len(tasks) == 0 && strings.TrimSpace(cfg.Prompt) != "" {
		tasks = []string{strings.TrimSpace(cfg.Prompt)}
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
		tasks:   tasks,
		naming:  newSessionNamer(mode, sessionID, nil),
		emitter: newEmitter(cfg.Output, cfg.Stream),
	}, nil
}

func (w *Worker) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	if err := ctx.Err(); err != nil {
		return agentkit.MessageEvent{}, err
	}
	w.mu.Lock()
	index := w.next
	if index >= len(w.tasks) {
		w.mu.Unlock()
		return agentkit.MessageEvent{}, io.EOF
	}
	w.next++
	w.mu.Unlock()

	task := w.tasks[index]
	return agentkit.MessageEvent{
		SessionID:  w.naming.forRun(index),
		PlatformID: "worker",
		Message:    userMessage(task),
	}, nil
}

func (w *Worker) Send(_ context.Context, event agentkit.OutboundEvent) error {
	return w.emitter.send(event)
}

func normalizeTasks(in []string) []string {
	out := make([]string, 0, len(in))
	for _, task := range in {
		if task = strings.TrimSpace(task); task != "" {
			out = append(out, task)
		}
	}
	return out
}

// positionalTask reads the command line tail so `agent -config worker.yaml "do
// it"` works without editing the config.
func positionalTask() []string {
	var args []string
	if flag.Parsed() {
		args = flag.Args()
	} else {
		args = os.Args[1:]
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return nil
	}
	return []string{strings.Join(args, " ")}
}
