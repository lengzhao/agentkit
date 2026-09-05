package runner_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/agentkit/runtime/runner"
)

func TestRunnerStopShutsDownTelemetry(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform:  &scriptedPlatform{},
		Loop:      &panickyLoop{},
		Telemetry: rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if rec.ShutdownCalls != 1 {
		t.Fatalf("shutdown calls = %d, want 1", rec.ShutdownCalls)
	}
}
