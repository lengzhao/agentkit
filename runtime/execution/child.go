// Package execution owns invocation lifecycles; context values live in rctx.
package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	capexecution "github.com/lengzhao/agentkit/cap/execution"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// ChildOptions is supplied by a trusted host, never decoded from model
// tool arguments. HistorySessionID must already be namespaced for the parent's
// workspace; this constructor does not select or generate persistent histories.
type ChildOptions struct {
	HistorySessionID agentkit.SessionID
	AgentID          agentkit.AgentID
}

type stepRegistrar interface {
	BeginStep(context.Context) (context.Context, func())
}

// NewChild creates a synchronous execution with separate history and
// parent-owned delivery/approval. Close only retires this execution's cancel
// registration; it never closes the parent broker or consumes parent steering.
// The parent context must identify a trusted workspace and logical conversation,
// Nested children inherit the original approval target and cancellation owner.
// Depth limits belong to the invoking host/tool. Async jobs need their own lifecycle.
func NewChild(parent context.Context, opts ChildOptions) (context.Context, context.CancelFunc, error) {
	env := rctx.EnvelopeFromContext(parent)
	if strings.TrimSpace(env.Workspace) == "" || strings.TrimSpace(env.Conversation) == "" {
		return nil, nil, fmt.Errorf("child execution requires parent workspace and conversation")
	}
	if strings.TrimSpace(string(opts.HistorySessionID)) == "" || strings.TrimSpace(string(opts.AgentID)) == "" {
		return nil, nil, fmt.Errorf("child execution requires history session and agent id")
	}
	if string(opts.HistorySessionID) == env.Conversation {
		return nil, nil, fmt.Errorf("child history must differ from parent conversation")
	}
	ctx := parent
	identity := NewIdentity(parent)
	ctx = rctx.WithExecutionIdentity(ctx, identity)
	endStep := func() {}
	if ctrl, ok := parent.Value(agentkit.KeySessionControl).(capexecution.Registrar); ok {
		ctx, endStep = ctrl.RegisterExecution(ctx, identity)
	} else if ctrl, ok := parent.Value(agentkit.KeySessionControl).(stepRegistrar); ok {
		ctx, endStep = ctrl.BeginStep(ctx)
	}
	ctx, cancel := context.WithCancel(ctx)
	var once sync.Once
	close := func() { once.Do(func() { cancel(); endStep() }) }

	ctrl := &childExecutionControl{
		ctx: ctx, broker: rtpermission.NewParentBroker(parent, opts.AgentID), parentControl: parent.Value(agentkit.KeySessionControl),
	}
	if scoped, ok := ctrl.parentControl.(interface{ ParentSessionControl() any }); ok {
		ctrl.parentControl = scoped.ParentSessionControl()
	}
	ctx = rctx.WithInheritedWorkingDirectory(ctx, workspace.WorkingDirectory(parent))
	ctx = rctx.WithoutSessionContext(ctx)
	// Custom Agent implementations consume the standard cwd context directly.
	// Preserve it without carrying the parent's history/epoch snapshot.
	ctx = workspace.WithWorkingDirectory(ctx, workspace.WorkingDirectory(parent))
	ctx = rctx.ApplyEnvelopeToContext(ctx, env.WithConversation(string(opts.HistorySessionID)).WithAgentID(opts.AgentID))
	// A child runtime opens its own session. Do not expose the parent's session
	// handle while preparing it, even though other inherited context values remain.
	ctx = context.WithValue(ctx, agentkit.KeySession, struct{}{})
	ctx = context.WithValue(ctx, agentkit.KeySessionControl, ctrl)
	ctx = context.WithValue(ctx, agentkit.KeyInSubagent, true)
	return ctx, close, nil
}

type childExecutionControl struct {
	ctx           context.Context
	broker        *rtpermission.ParentBroker
	parentControl any
}

func (c *childExecutionControl) PermissionCapability() permission.Capability {
	return c.broker.PermissionCapability()
}

func (c *childExecutionControl) Await(ctx context.Context, req permission.Request) (permission.Result, error) {
	ctx, done := c.boundContext(ctx)
	defer done()
	return c.broker.Await(ctx, req)
}

func (c *childExecutionControl) boundContext(parent context.Context) (context.Context, func()) {
	endDeadline := func() {}
	if deadline, ok := c.ctx.Deadline(); ok {
		parent, endDeadline = context.WithDeadline(parent, deadline)
	}
	ctx, cancel := context.WithCancelCause(parent)
	propagate := func() {
		// The deadline context above owns timeout classification. Cancelling it
		// here could race its timer and turn DeadlineExceeded into Canceled.
		if !errors.Is(c.ctx.Err(), context.DeadlineExceeded) {
			cancel(context.Cause(c.ctx))
		}
	}
	stop := context.AfterFunc(c.ctx, propagate)
	if c.ctx.Err() != nil {
		propagate()
	}
	return ctx, func() { stop(); cancel(context.Canceled); endDeadline() }
}

// ParentPermissionBroker lets nested scopes capture the original interaction
// directly rather than changing the acting agent while chaining proxies.
func (c *childExecutionControl) ParentPermissionBroker() *rtpermission.ParentBroker { return c.broker }

// ParentSessionControl is used only by existing background-job adapters. A job
// must not retain a synchronous scope that its caller is about to close.
func (c *childExecutionControl) ParentSessionControl() any { return c.parentControl }

func (c *childExecutionControl) BeginStep(parent context.Context) (context.Context, func()) {
	return c.boundContext(parent)
}

// Child runtimes get no access to parent steering or destructive cancel reads.
func (c *childExecutionControl) ClearTurnCancel() {}
func (c *childExecutionControl) PopCancelReason() string {
	if err := context.Cause(c.ctx); err != nil {
		return err.Error()
	}
	return ""
}
func (c *childExecutionControl) PopSteering() []agentkit.SteerEntry    { return nil }
func (c *childExecutionControl) RestoreSteering([]agentkit.SteerEntry) {}
func (c *childExecutionControl) HasSteering() bool                     { return false }
