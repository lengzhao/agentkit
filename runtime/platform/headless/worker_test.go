package headless_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/headless"
)

// newWorker builds a worker without a schedule registry, i.e. batch mode.
func newWorker(cfg headless.WorkerConfig) (agentkit.Platform, error) {
	return headless.NewWorker(cfg, headless.WorkerDeps{})
}

// tasks builds plain run-once task specs from prompts.
func tasks(prompts ...string) []headless.TaskSpec {
	out := make([]headless.TaskSpec, 0, len(prompts))
	for _, prompt := range prompts {
		out = append(out, headless.TaskSpec{Prompt: prompt})
	}
	return out
}

func textOfMessage(msg agentkit.ModelMessage) string {
	var b strings.Builder
	for _, part := range msg.Content {
		b.WriteString(part.Text)
	}
	return b.String()
}

func TestWorkerRunsEachTaskThenReportsEOF(t *testing.T) {
	t.Parallel()

	p, err := newWorker(headless.WorkerConfig{
		Tasks: tasks("first", "  second  ", "", "third"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	var got []string
	var sessions []agentkit.SessionID
	for {
		event, err := p.Receive(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		got = append(got, textOfMessage(event.Message))
		sessions = append(sessions, event.SessionID)
		if event.PlatformID != "worker" {
			t.Fatalf("platform id = %q, want worker", event.PlatformID)
		}
	}

	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("tasks = %v, want %v (blank entries dropped)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("task[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Fresh mode: every task gets its own session.
	seen := map[agentkit.SessionID]bool{}
	for _, id := range sessions {
		if seen[id] {
			t.Fatalf("session id %q reused in fresh mode: %v", id, sessions)
		}
		seen[id] = true
	}

	// A second Receive after EOF must stay at EOF.
	if _, err := p.Receive(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("post-EOF receive err = %v, want EOF", err)
	}
}

func TestWorkerFixedModeSharesOneSession(t *testing.T) {
	t.Parallel()

	p, err := newWorker(headless.WorkerConfig{
		Tasks:       tasks("a", "b"),
		SessionMode: headless.SessionFixed,
		SessionID:   "batch",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	first, err := p.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID != second.SessionID {
		t.Fatalf("fixed mode gave different sessions: %q vs %q", first.SessionID, second.SessionID)
	}
	if !strings.HasPrefix(string(first.SessionID), "batch") {
		t.Fatalf("session id = %q, want the configured prefix", first.SessionID)
	}
}

func TestWorkerFreshSessionsAreUniqueAcrossProcesses(t *testing.T) {
	t.Parallel()

	// Two workers stand in for two process runs. A bare run counter would give
	// both "worker:run-1" and the second run would reopen the first run's
	// history, which is the opposite of "fresh".
	ids := make([]agentkit.SessionID, 0, 2)
	for i := 0; i < 2; i++ {
		p, err := newWorker(headless.WorkerConfig{Tasks: tasks("task")})
		if err != nil {
			t.Fatal(err)
		}
		event, err := p.Receive(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, event.SessionID)
	}
	if ids[0] == ids[1] {
		t.Fatalf("two runs produced the same fresh session id %q", ids[0])
	}
}

func TestWorkerRequiresATask(t *testing.T) {
	t.Parallel()

	if _, err := newWorker(headless.WorkerConfig{}); err == nil {
		t.Fatal("expected an error with no task configured")
	}
	if _, err := newWorker(headless.WorkerConfig{SessionMode: "weird", Prompt: "x"}); err == nil {
		t.Fatal("expected an error for an unknown sessionMode")
	}
}

func TestWorkerPromptIsTheSingleTaskShorthand(t *testing.T) {
	t.Parallel()

	p, err := newWorker(headless.WorkerConfig{Prompt: "just this"})
	if err != nil {
		t.Fatal(err)
	}
	event, err := p.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := textOfMessage(event.Message); got != "just this" {
		t.Fatalf("task = %q, want %q", got, "just this")
	}
	if _, err := p.Receive(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("second receive err = %v, want EOF", err)
	}
}

func TestWorkerHonorsCancellation(t *testing.T) {
	t.Parallel()

	p, err := newWorker(headless.WorkerConfig{Tasks: tasks("a")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Receive(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("receive err = %v, want context.Canceled", err)
	}
}
