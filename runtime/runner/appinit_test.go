package runner_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/runner"
	"github.com/lengzhao/pluginkit/build"
)

type initRecorder struct {
	calls []string
	err   error
}

func (r *initRecorder) InitApp(context.Context) error {
	r.calls = append(r.calls, "init")
	return r.err
}

type noopPlatform struct{}

func (noopPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	return agentkit.MessageEvent{}, io.EOF
}

func (noopPlatform) Send(context.Context, agentkit.OutboundEvent) error { return nil }

func TestRunRunsAppInitBeforeServe(t *testing.T) {
	t.Parallel()

	rec := &initRecorder{}
	root, err := runner.New(runner.Config{}, runner.Deps{Platform: noopPlatform{}, Loop: &panickyLoop{}})
	if err != nil {
		t.Fatal(err)
	}
	result := &build.Result{Instances: []build.Instance{{ID: "bootstrap.test", Use: "bootstrap/test", Value: rec}}}
	if err := root.Run(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("InitApp calls = %d, want 1", len(rec.calls))
	}
}

func TestRunAppInitFailureAbortsServe(t *testing.T) {
	t.Parallel()

	rec := &initRecorder{err: errors.New("boom")}
	root, err := runner.New(runner.Config{}, runner.Deps{Platform: &blockingPlatform{}, Loop: &panickyLoop{}})
	if err != nil {
		t.Fatal(err)
	}
	result := &build.Result{Instances: []build.Instance{{ID: "bootstrap.test", Use: "bootstrap/test", Value: rec}}}
	err = root.Run(context.Background(), result)
	if err == nil {
		t.Fatal("expected init failure")
	}
}

type blockingPlatform struct {
	called bool
}

func (p *blockingPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	p.called = true
	select {}
}

func (p *blockingPlatform) Send(context.Context, agentkit.OutboundEvent) error { return nil }
