package execution_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/lengzhao/agentkit"
	capexecution "github.com/lengzhao/agentkit/cap/execution"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/execution"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func childParentContext() context.Context {
	return rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: "parent", Workspace: "tenant-a", AgentID: "assistant",
		Route: rctx.SessionRoute("cli", "delivery"), Actor: agentkit.ActorRef{UserID: "user"},
	})
}

func newChild(t *testing.T, parent context.Context) context.Context {
	t.Helper()
	ctx, close, err := execution.NewChild(parent, execution.ChildOptions{HistorySessionID: "child", AgentID: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(close)
	return ctx
}

func TestChildExecutionSeparatesHistoryAndDelivery(t *testing.T) {
	parent := childParentContext()
	parent = rctx.WithExecution(parent, rctx.Execution{SessionID: "parent", Cwd: "/parent-dir", Epoch: "parent-epoch"})
	parent = context.WithValue(parent, agentkit.KeyTurnID, "parent-turn")
	ctx := newChild(t, parent)
	env := rctx.EnvelopeFromContext(ctx)
	if env.Conversation != "child" || env.AgentID != "reviewer" || env.Workspace != "tenant-a" || env.Actor.UserID != "user" {
		t.Fatalf("child envelope: %+v", env)
	}
	if !reflect.DeepEqual(env.Route, rctx.EnvelopeFromContext(parent).Route) {
		t.Fatal("delivery changed")
	}
	if execution, _ := rctx.ExecutionFromContext(ctx); execution.Epoch != "" || execution.SessionID != "" {
		t.Fatalf("parent execution leaked: %+v", execution)
	}
	if workspace.WorkingDirectory(ctx) != "/parent-dir" || rctx.InheritedWorkingDirectory(ctx) != "/parent-dir" {
		t.Fatal("standard cwd must remain available without inheriting the parent history snapshot")
	}
	if ctx.Value(agentkit.KeyTurnID) != "parent-turn" {
		t.Fatal("parent turn association lost")
	}
	if rctx.SessionIDFromContext(parent) != "parent" {
		t.Fatal("mutated parent")
	}
}

func TestChildExecutionIdentityDoesNotDependOnHistory(t *testing.T) {
	parentIdentity := capexecution.Identity{ID: "parent-execution", RootTurnID: "root-turn"}
	parent := rctx.WithExecutionIdentity(childParentContext(), parentIdentity)
	parent = context.WithValue(parent, agentkit.KeyTurnID, "backend-telemetry-turn")
	first := newChild(t, parent)
	second := newChild(t, parent)
	a, b := rctx.ExecutionIdentityFromContext(first), rctx.ExecutionIdentityFromContext(second)
	if a.ID == "" || a.ID == b.ID || a.ParentID != parentIdentity.ID || a.RootTurnID != "root-turn" || b.RootTurnID != "root-turn" {
		t.Fatalf("first=%+v second=%+v", a, b)
	}
	if rctx.SessionIDFromContext(first) != rctx.SessionIDFromContext(second) {
		t.Fatal("test must use same history")
	}
	if rctx.ExecutionIdentityFromContext(parent) != parentIdentity {
		t.Fatal("parent identity mutated")
	}
}

func TestChildExecutionDoesNotConsumeParentState(t *testing.T) {
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	parent := context.WithValue(childParentContext(), agentkit.KeySessionControl, ctrl)
	_ = ctrl.Steer(parent, agentkit.ModelMessage{Role: "user"})
	_ = ctrl.Cancel(parent, "stop before child")
	ctx := newChild(t, parent)
	local := ctx.Value(agentkit.KeySessionControl).(interface {
		ClearTurnCancel()
		BeginStep(context.Context) (context.Context, func())
		PopCancelReason() string
		PopSteering() []agentkit.SteerEntry
		HasSteering() bool
	})
	local.ClearTurnCancel()
	_ = local.PopCancelReason()
	if local.HasSteering() || len(local.PopSteering()) != 0 {
		t.Fatal("child consumed parent steering")
	}
	if !ctrl.HasSteering() || ctrl.PopCancelReason() != "stop before child" {
		t.Fatal("child mutated parent control state")
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("pending stop did not cancel child")
	}
}

func TestChildExecutionCleanupLeavesParentRegistered(t *testing.T) {
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	parent := context.WithValue(childParentContext(), agentkit.KeySessionControl, ctrl)
	parentStep, parentEnd := ctrl.BeginStep(parent)
	defer parentEnd()
	ctx, close, err := execution.NewChild(parentStep, execution.ChildOptions{HistorySessionID: "child", AgentID: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	close()
	close()
	if ctx.Err() != context.Canceled || parentStep.Err() != nil {
		t.Fatal("cleanup escaped child scope")
	}
	_ = ctrl.Cancel(parent, "parent still owns stop")
	if parentStep.Err() != context.Canceled {
		t.Fatal("child cleanup unregistered parent")
	}
}

func TestChildExecutionParentStopCancelsChild(t *testing.T) {
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	parent := context.WithValue(childParentContext(), agentkit.KeySessionControl, ctrl)
	ctx := newChild(t, parent)
	_ = ctrl.Cancel(parent, "user stop")
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("parent stop did not reach child")
	}
}

type capturingParentBroker struct {
	*loop.Control
	contexts []agentkit.TurnEnvelope
	ids      []string
}

func (b *capturingParentBroker) PermissionCapability() permission.Capability {
	return permission.Capability{Interactive: true}
}
func (b *capturingParentBroker) Await(ctx context.Context, req permission.Request) (permission.Result, error) {
	b.contexts = append(b.contexts, rctx.EnvelopeFromContext(ctx))
	b.ids = append(b.ids, req.ID)
	return permission.Result{Outcome: permission.OutcomeResolved, Allow: false}, nil
}

func TestChildExecutionPermissionUsesParentConversationAndChildIdentity(t *testing.T) {
	b := &capturingParentBroker{Control: loop.NewControl()}
	defer b.Stop()
	parent := context.WithValue(childParentContext(), agentkit.KeySessionControl, b)
	ctx := newChild(t, parent)
	broker, ok := permission.BrokerFrom(ctx)
	if !ok || !permission.CapabilityFrom(ctx).Interactive {
		t.Fatal("parent broker capability lost")
	}
	for range 2 {
		result, err := broker.Await(ctx, permission.Request{ID: "remote-reused-id", Kind: permission.KindAllowDeny})
		if err != nil || result.Allow || result.Outcome != permission.OutcomeResolved {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	for _, env := range b.contexts {
		if env.Conversation != "parent" || env.AgentID != "reviewer" || env.Workspace != "tenant-a" || !reflect.DeepEqual(env.Route, rctx.EnvelopeFromContext(parent).Route) {
			t.Fatalf("approval scope=%+v", env)
		}
	}
	if b.ids[0] == b.ids[1] || b.ids[0] == "remote-reused-id" {
		t.Fatal("reused remote request ID can accept a late reply")
	}
}

func TestChildExecutionWithoutBrokerDoesNotApprove(t *testing.T) {
	ctx := newChild(t, childParentContext())
	broker, ok := permission.BrokerFrom(ctx)
	if !ok {
		t.Fatal("missing explicit no-human broker")
	}
	result, err := broker.Await(ctx, permission.Request{Kind: permission.KindAllowDeny})
	if err != nil || result.Allow || result.Outcome != permission.OutcomeNoHuman {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestChildExecutionRejectsInvalidScopes(t *testing.T) {
	for _, opts := range []execution.ChildOptions{{}, {HistorySessionID: "child"}, {HistorySessionID: "parent", AgentID: "reviewer"}} {
		if _, close, err := execution.NewChild(childParentContext(), opts); err == nil {
			close()
			t.Fatalf("accepted %+v", opts)
		}
	}
	if _, close, err := execution.NewChild(context.Background(), execution.ChildOptions{HistorySessionID: "child", AgentID: "reviewer"}); err == nil {
		close()
		t.Fatal("accepted parent without trusted scope")
	}

}

func TestChildExecutionRequiresExplicitWorkspace(t *testing.T) {
	for _, parent := range []context.Context{
		rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "local-parent"}),
		rctx.WithWorkspace(childParentContext(), ""),
	} {
		if _, close, err := execution.NewChild(parent, execution.ChildOptions{HistorySessionID: "child", AgentID: "reviewer"}); err == nil {
			close()
			t.Fatal("strict scope accepted a parent without an explicit workspace")
		}
	}
}

func TestNestedChildApprovalKeepsRootInteractionAndLeafIdentity(t *testing.T) {
	b := &capturingParentBroker{Control: loop.NewControl()}
	defer b.Stop()
	parent := context.WithValue(childParentContext(), agentkit.KeySessionControl, b)
	parent = rctx.WithExecutionIdentity(parent, capexecution.Identity{ID: "root", RootTurnID: "turn"})
	first, closeFirst, err := execution.NewChild(parent, execution.ChildOptions{HistorySessionID: "first", AgentID: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	defer closeFirst()
	second, closeSecond, err := execution.NewChild(first, execution.ChildOptions{HistorySessionID: "second", AgentID: "writer"})
	if err != nil {
		t.Fatal(err)
	}
	defer closeSecond()
	a, c := rctx.ExecutionIdentityFromContext(first), rctx.ExecutionIdentityFromContext(second)
	if c.ParentID != a.ID || c.RootTurnID != "turn" {
		t.Fatalf("nested identity=%+v", c)
	}
	broker, _ := permission.BrokerFrom(second)
	if _, err := broker.Await(second, permission.Request{Kind: permission.KindAllowDeny}); err != nil {
		t.Fatal(err)
	}
	if len(b.contexts) != 1 || b.contexts[0].Conversation != "parent" || b.contexts[0].AgentID != "writer" {
		t.Fatalf("root approvals=%+v", b.contexts)
	}
	closeSecond()
	if first.Err() != nil {
		t.Fatal("nested cleanup cancelled parent scope")
	}
	_ = b.Cancel(parent, "stop root")
	if first.Err() != context.Canceled {
		t.Fatal("nested cleanup removed root cancellation")
	}
}
