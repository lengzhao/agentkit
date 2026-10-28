package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/execution"
)

func TestChildStepPreservesScopeCancellationCause(t *testing.T) {
	parent, cancel := context.WithCancelCause(childParentContext())
	defer cancel(nil)
	child := newChild(t, parent)
	ctrl := child.Value(agentkit.KeySessionControl).(interface {
		BeginStep(context.Context) (context.Context, func())
	})
	step, end := ctrl.BeginStep(context.WithoutCancel(child))
	defer end()
	want := errors.New("scope cancelled by owner")
	cancel(want)
	select {
	case <-step.Done():
	case <-time.After(time.Second):
		t.Fatal("child cancellation not propagated")
	}
	if !errors.Is(context.Cause(step), want) {
		t.Fatalf("cause=%v", context.Cause(step))
	}
}

func TestChildStepPreservesScopeDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(childParentContext(), 30*time.Millisecond)
	defer cancel()
	child, close, err := execution.NewChild(parent, execution.ChildOptions{HistorySessionID: "child", AgentID: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	ctrl := child.Value(agentkit.KeySessionControl).(interface {
		BeginStep(context.Context) (context.Context, func())
	})
	step, end := ctrl.BeginStep(context.WithoutCancel(child))
	defer end()
	if _, ok := step.Deadline(); !ok {
		t.Error("scope deadline not exposed to backend callback")
	}
	select {
	case <-step.Done():
	case <-time.After(time.Second):
		t.Fatal("deadline not propagated")
	}
	if !errors.Is(step.Err(), context.DeadlineExceeded) || !errors.Is(context.Cause(step), context.DeadlineExceeded) {
		t.Fatalf("err=%v cause=%v", step.Err(), context.Cause(step))
	}
}
