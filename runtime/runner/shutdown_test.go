package runner_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	rtloop "github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/runner"
	"github.com/lengzhao/pluginkit/build"
)

type shutdownCounter struct{ calls int }

func (h *shutdownCounter) ShutdownHooks() []func() { return []func(){func() { h.calls++ }} }

type cancelledPlatform struct{ agentkit.Platform }

func (cancelledPlatform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	return agentkit.MessageEvent{}, ctx.Err()
}

func TestSignalShutdownHooksRunOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hooks := &shutdownCounter{}
	root, err := runner.New(runner.Config{}, runner.Deps{Platform: cancelledPlatform{}, Loop: &panickyLoop{}})
	if err != nil {
		t.Fatal(err)
	}
	result := &build.Result{Instances: []build.Instance{{ID: "shutdown", Use: "test", Value: hooks}}}
	_ = root.Run(ctx, result)
	if err := root.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hooks.calls != 1 {
		t.Fatalf("shutdown hook calls = %d, want 1", hooks.calls)
	}
}

// Simulate a tool that returns only after Run has abandoned in-flight work.
type lateCleanupAgent struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (*lateCleanupAgent) ID() agentkit.AgentID { return "late-cleanup" }
func (a *lateCleanupAgent) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	ctrl := ctx.Value(agentkit.KeySessionControl).(interface {
		BeginStep(context.Context) (context.Context, func())
	})
	_, end := ctrl.BeginStep(ctx)
	close(a.started)
	<-a.release
	end()
	close(a.finished)
	return ctx.Err()
}

type signalPlatform struct {
	sent bool
}

func (p *signalPlatform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	if !p.sent {
		p.sent = true
		return userEvent("late-cleanup", "go"), nil
	}
	<-ctx.Done()
	return agentkit.MessageEvent{}, ctx.Err()
}
func (*signalPlatform) Send(context.Context, agentkit.OutboundEvent) error { return nil }

func TestSignalShutdownAllowsLateTurnCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ag := &lateCleanupAgent{started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	defer close(ag.release)
	loop, err := rtloop.New(rtloop.Config{}, rtloop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}
	root, err := runner.New(runner.Config{}, runner.Deps{Platform: &signalPlatform{}, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- root.Run(ctx, nil) }()
	select {
	case <-ag.started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not start")
	}
	cancel()
	select {
	case err := <-stopped:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("signal shutdown waited for uncooperative turn")
	}
	// Release without closing: deferred close also releases on test failure.
	ag.release <- struct{}{}
	select {
	case <-ag.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("late step cleanup did not finish safely")
	}
}
