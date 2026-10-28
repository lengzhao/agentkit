package loop_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capexecution "github.com/lengzhao/agentkit/cap/execution"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestLoopCancelInterruptsBusySession(t *testing.T) {
	t.Parallel()

	agents := []agentkit.Agent{blockingAgent{}}
	l, err := loop.New(loop.Config{}, loop.Deps{Agents: agents})
	if err != nil {
		t.Fatal(err)
	}

	sessionID := agentkit.SessionID("cancel-test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- l.Dispatch(ctx, agenttest.LoopRequest(sessionID, agentkit.MessageEvent{
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
			},
		}))
	}()

	waitUntil(t, func() bool { return l.IsSessionBusy(sessionID) })

	cancelCtx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(sessionID), Workspace: string(sessionID)})
	if err := l.Cancel(cancelCtx, "/stop"); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancelled turn error")
		}
		if !errors.Is(err, context.Canceled) && err.Error() != "cancelled: /stop" {
			t.Fatalf("dispatch err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch did not finish after cancel")
	}
	if l.IsSessionBusy(sessionID) {
		t.Fatal("session still busy after cancel")
	}
}

func waitUntil(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

type blockingAgent struct{}

func (blockingAgent) ID() agentkit.AgentID { return "blocker" }

func (blockingAgent) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	ctrl, ok := ctx.Value(agentkit.KeySessionControl).(interface {
		BeginStep(context.Context) (context.Context, func())
	})
	if !ok {
		return errors.New("missing session control")
	}
	stepCtx, endStep := ctrl.BeginStep(ctx)
	defer endStep()
	<-stepCtx.Done()
	return stepCtx.Err()
}

var _ agentkit.Agent = blockingAgent{}

func TestControlCancelCancelsStepContext(t *testing.T) {
	t.Parallel()

	ctrl := loop.NewControl()
	parent := context.Background()
	stepCtx, endStep := ctrl.BeginStep(parent)
	defer endStep()

	done := make(chan struct{})
	go func() {
		<-stepCtx.Done()
		close(done)
	}()

	if err := ctrl.Cancel(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("step context not cancelled")
	}
}

func TestControlCancelIsIdempotent(t *testing.T) {
	t.Parallel()

	ctrl := loop.NewControl()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		stepCtx, endStep := ctrl.BeginStep(context.Background())
		defer endStep()
		<-stepCtx.Done()
	}()

	time.Sleep(20 * time.Millisecond)
	if err := ctrl.Cancel(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Cancel(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func TestControlOldCleanupDoesNotUnregisterNewStep(t *testing.T) {
	t.Parallel()
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	_, oldEnd := ctrl.BeginStep(context.Background())
	next, nextEnd := ctrl.BeginStep(context.Background())
	defer nextEnd()
	oldEnd()
	oldEnd() // Cleanup is idempotent and owns only its original registration.
	if err := ctrl.Cancel(context.Background(), "stop next"); err != nil {
		t.Fatal(err)
	}
	if next.Err() != context.Canceled {
		t.Fatal("old cleanup removed the new step's cancellation registration")
	}
}

func TestControlCancelReachesAllActiveSteps(t *testing.T) {
	t.Parallel()
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	parent, endParent := ctrl.BeginStep(context.Background())
	defer endParent()
	child, endChild := ctrl.BeginStep(context.Background())
	defer endChild()
	if err := ctrl.Cancel(context.Background(), "stop both"); err != nil {
		t.Fatal(err)
	}
	if parent.Err() != context.Canceled || child.Err() != context.Canceled {
		t.Fatalf("parent=%v child=%v; both active registrations must cancel", parent.Err(), child.Err())
	}
}

func TestControlStepHonorsPendingStop(t *testing.T) {
	t.Parallel()
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	if err := ctrl.Cancel(context.Background(), "stop before start"); err != nil {
		t.Fatal(err)
	}
	step, end := ctrl.BeginStep(context.Background())
	defer end()
	if step.Err() != context.Canceled {
		t.Fatal("step started despite pending stop")
	}
	if got := ctrl.PopCancelReason(); got != "stop before start" {
		t.Fatalf("registration consumed parent cancel reason: %q", got)
	}
}

func TestControlCancelRootIsolatesExecutionOwners(t *testing.T) {
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	a, endA := ctrl.RegisterExecution(context.Background(), capexecution.Identity{ID: "a", RootTurnID: "turn-a"})
	defer endA()
	b, endB := ctrl.RegisterExecution(context.Background(), capexecution.Identity{ID: "b", RootTurnID: "turn-b"})
	defer endB()
	if !ctrl.CancelRoot("turn-a", "stop a") {
		t.Fatal("root registration not found")
	}
	if a.Err() != context.Canceled || b.Err() != nil {
		t.Fatalf("a=%v b=%v", a.Err(), b.Err())
	}
	endA()
	if err := ctrl.Cancel(context.Background(), "stop remaining"); err != nil {
		t.Fatal(err)
	}
	if b.Err() != context.Canceled {
		t.Fatal("old cleanup removed another owner")
	}
	if ctrl.CancelRoot("", "invalid") {
		t.Fatal("empty root may not target legacy registrations")
	}
}
