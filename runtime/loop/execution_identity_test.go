package loop

import (
	"context"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capexecution "github.com/lengzhao/agentkit/cap/execution"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type identityProbeAgent struct{ seen []capexecution.Identity }

func (*identityProbeAgent) ID() agentkit.AgentID { return "identity-probe" }
func (a *identityProbeAgent) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	a.seen = append(a.seen, rctx.ExecutionIdentityFromContext(ctx))
	return nil
}

type stoppedTurnAgent struct {
	ready chan struct{}
	calls int
}

func (*stoppedTurnAgent) ID() agentkit.AgentID { return "stop-once" }
func (a *stoppedTurnAgent) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	control := ctx.Value(agentkit.KeySessionControl).(interface {
		BeginStep(context.Context) (context.Context, func())
	})
	step, end := control.BeginStep(ctx)
	defer end()
	a.calls++
	if a.calls == 1 {
		close(a.ready)
		<-step.Done()
	}
	return step.Err()
}

func TestLoopStoppedTurnDoesNotPoisonNextCustomAgentTurn(t *testing.T) {
	ag := &stoppedTurnAgent{ready: make(chan struct{})}
	l, err := New(Config{}, Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.(*Default).Stop(context.Background())
	req := agentkit.LoopRequest{Event: agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Conversation: "same-history", Workspace: "tenant"}}}
	ended := make(chan error, 1)
	go func() { ended <- l.Dispatch(context.Background(), req) }()
	select {
	case <-ag.ready:
	case <-time.After(time.Second):
		t.Fatal("first turn did not start")
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), req.Event.Envelope)
	if err := l.Cancel(ctx, "stop first turn"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("first turn did not stop")
	}
	if err := l.Dispatch(context.Background(), req); err != nil {
		t.Fatalf("previous turn's stop leaked into next turn: %v", err)
	}
}

func TestLateTurnCleanupDoesNotClearAnotherTurnsStop(t *testing.T) {
	ctrl := NewControl()
	defer ctrl.Stop()
	endOld := ctrl.beginTurn("old")
	endNew := ctrl.beginTurn("new")
	_ = ctrl.Cancel(context.Background(), "stop new")
	endOld()
	if got := ctrl.PopCancelReason(); got != "stop new" {
		t.Fatalf("old cleanup cleared next turn's reason: %q", got)
	}
	endNew()
}

func TestLoopAssignsIdentityPerInvocation(t *testing.T) {
	ag := &identityProbeAgent{}
	l, err := New(Config{}, Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.(*Default).Stop(context.Background())
	req := agentkit.LoopRequest{Event: agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Conversation: "history", Workspace: "tenant"}}}
	for range 2 {
		if err := l.Dispatch(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	a, b := ag.seen[0], ag.seen[1]
	if a.ID == "" || a.ID == b.ID || a.RootTurnID == "" || a.RootTurnID == b.RootTurnID || a.ParentID != "" {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}
