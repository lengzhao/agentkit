package loop_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/loop"
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
		errCh <- l.Dispatch(ctx, agentkit.LoopRequest{
			Event: agentkit.MessageEvent{
				SessionID: sessionID,
				Message: agentkit.ModelMessage{
					Role:    "user",
					Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
				},
			},
		})
	}()

	waitUntil(t, func() bool { return l.IsSessionBusy(sessionID) })

	cancelCtx := context.WithValue(context.Background(), agentkit.KeySessionID, sessionID)
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
