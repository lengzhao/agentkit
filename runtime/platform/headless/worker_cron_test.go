package headless_test

import (
	"testing"

	"github.com/lengzhao/agentkit/runtime/platform/headless"
)

func TestWorkerRejectsCronTasks(t *testing.T) {
	t.Parallel()

	_, err := headless.NewWorker(headless.WorkerConfig{
		Tasks: []headless.TaskSpec{{Prompt: "nightly", Cron: "@daily"}},
	}, headless.WorkerDeps{})
	if err == nil {
		t.Fatal("expected an error: cron tasks belong in schedule/cron")
	}
}

func TestWorkerRejectsCronWithoutPrompt(t *testing.T) {
	t.Parallel()

	_, err := headless.NewWorker(headless.WorkerConfig{
		Tasks: []headless.TaskSpec{{Cron: "@daily"}},
	}, headless.WorkerDeps{})
	if err == nil {
		t.Fatal("expected an error for a cron task with no prompt")
	}
}
