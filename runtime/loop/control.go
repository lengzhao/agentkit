package loop

import (
	"context"
	"errors"
	"sync"

	"github.com/lengzhao/agentkit"
	capexecution "github.com/lengzhao/agentkit/cap/execution"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// Control holds steer / follow-up queues and step-cancel state for one session.
// Loop owns one Control per SessionID; Agent reads step-level hooks via
// ctx.Value(agentkit.KeySessionControl).
//
// All mutable state is owned by a single goroutine; callers synchronize via ch.
type Control struct {
	ch       chan func(*controlState)
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

type controlState struct {
	stepCancels       map[uint64]executionRegistration
	nextStepID        uint64
	steering          []agentkit.SteerEntry
	followUps         []agentkit.ModelMessage
	cancelReason      string
	activeExecutionID string
	cancelExecutionID string
	capability        permission.Capability
	permissionPending *pendingPermission
	// permissionAllowAll is set when the user replies "allow_all" (全部允许):
	// subsequent KindAllowDeny requests in this session auto-resolve to allow.
	permissionAllowAll bool
}

type executionRegistration struct {
	identity capexecution.Identity
	cancel   context.CancelCauseFunc
}

func NewControl() *Control {
	c := &Control{ch: make(chan func(*controlState)), stop: make(chan struct{}), done: make(chan struct{})}
	go c.loop()
	return c
}

func (c *Control) loop() {
	var st controlState
	defer close(c.done)
	defer func() {
		if st.permissionPending != nil {
			st.permissionPending.finish()
		}
		for _, registration := range st.stepCancels {
			registration.cancel(context.Canceled)
		}
	}()
	for {
		select {
		case fn := <-c.ch:
			fn(&st)
		case <-c.stop:
			return
		}
	}
}

// Stop cancels pending work and retires the owner. Concurrent calls and late
// turn cleanup are safe: the request channel stays open, and stop rejects sends.
func (c *Control) Stop() {
	c.stopOnce.Do(func() { close(c.stop) })
	<-c.done
}

// sync reports whether the owner accepted the operation. Once accepted, wait
// for its completion even during shutdown so callers can safely read results.
func (c *Control) sync(fn func(*controlState)) bool {
	select {
	case <-c.stop:
		return false
	default:
	}
	done := make(chan struct{})
	select {
	case c.ch <- func(st *controlState) { fn(st); close(done) }:
		<-done
		return true
	case <-c.stop:
		return false
	}
}

func (c *Control) setTurnCapability(cap permission.Capability) {
	c.sync(func(st *controlState) {
		st.capability = cap
	})
}

func (c *Control) PermissionCapability() permission.Capability {
	var cap permission.Capability
	c.sync(func(st *controlState) {
		cap = st.capability
	})
	return cap
}

func (c *Control) Steer(_ context.Context, msg agentkit.ModelMessage) error {
	c.sync(func(st *controlState) {
		st.steering = append(st.steering, agentkit.SteerEntry{Message: msg})
	})
	return nil
}

// SteerEntry queues one steering message together with its outcome reporter.
// The reporter is consumed when the queue position is resolved: locally by the
// agent injecting at a step boundary, or by Loop running it as the next turn.
func (c *Control) SteerEntry(_ context.Context, entry agentkit.SteerEntry) error {
	c.sync(func(st *controlState) {
		st.steering = append(st.steering, entry)
	})
	return nil
}

func (c *Control) FollowUp(_ context.Context, msg agentkit.ModelMessage) error {
	c.sync(func(st *controlState) {
		st.followUps = append(st.followUps, msg)
	})
	return nil
}

func (c *Control) Cancel(_ context.Context, reason string) error {
	c.sync(func(st *controlState) {
		st.cancelReason = reason
		st.cancelExecutionID = st.activeExecutionID
		for _, registration := range st.stepCancels {
			registration.cancel(cancelCause(reason))
		}
	})
	return nil
}

func (c *Control) DrainFollowUps(_ context.Context, mode agentkit.FollowUpMode) ([]agentkit.ModelMessage, error) {
	var out []agentkit.ModelMessage
	c.sync(func(st *controlState) {
		if len(st.followUps) == 0 {
			return
		}
		switch mode {
		case agentkit.FollowUpAll:
			out = append([]agentkit.ModelMessage(nil), st.followUps...)
			st.followUps = nil
		default:
			out = []agentkit.ModelMessage{st.followUps[0]}
			st.followUps = st.followUps[1:]
		}
	})
	return out, nil
}

func (c *Control) ClearTurnCancel() {
	c.sync(func(st *controlState) {
		st.cancelReason = ""
		st.cancelExecutionID = ""
	})
}

// beginTurn bounds a pending stop to one loop invocation. Custom agents are
// not required to consume PopCancelReason; their next turn must still run.
// An idle/pre-start stop has no owner and is adopted by the upcoming turn.
func (c *Control) beginTurn(executionID string) func() {
	c.sync(func(st *controlState) {
		if st.cancelExecutionID != "" && st.cancelExecutionID != executionID {
			st.cancelReason = ""
			st.cancelExecutionID = ""
		}
		st.activeExecutionID = executionID
		if st.cancelReason != "" {
			st.cancelExecutionID = executionID
		}
	})
	return func() {
		c.sync(func(st *controlState) {
			if st.activeExecutionID != executionID {
				return
			}
			st.activeExecutionID = ""
			if st.cancelExecutionID == executionID {
				st.cancelReason = ""
				st.cancelExecutionID = ""
			}
		})
	}
}

func (c *Control) BeginStep(parent context.Context) (context.Context, func()) {
	return c.RegisterExecution(parent, rctx.ExecutionIdentityFromContext(parent))
}

// RegisterExecution associates this cancel registration with an immutable
// invocation owner. Legacy BeginStep delegates here without changing its API.
func (c *Control) RegisterExecution(parent context.Context, identity capexecution.Identity) (context.Context, func()) {
	stepCtx, cancel := context.WithCancelCause(rctx.WithExecutionIdentity(parent, identity))
	var id uint64
	if !c.sync(func(st *controlState) {
		st.nextStepID++
		id = st.nextStepID
		if st.stepCancels == nil {
			st.stepCancels = make(map[uint64]executionRegistration)
		}
		st.stepCancels[id] = executionRegistration{identity: identity, cancel: cancel}
		if st.cancelReason != "" {
			cancel(cancelCause(st.cancelReason))
		}
	}) {
		cancel(context.Canceled)
	}
	var once sync.Once
	return stepCtx, func() {
		once.Do(func() {
			cancel(context.Canceled)
			c.sync(func(st *controlState) {
				delete(st.stepCancels, id)
			})
		})
	}
}

// CancelRoot cancels only registrations owned by the given user interaction.
// It does not set the session-wide pending reason or stop another root/job.
func (c *Control) CancelRoot(rootTurnID, reason string) bool {
	if rootTurnID == "" {
		return false
	}
	found := false
	c.sync(func(st *controlState) {
		for _, registration := range st.stepCancels {
			if registration.identity.RootTurnID == rootTurnID {
				found = true
				registration.cancel(cancelCause(reason))
			}
		}
	})
	return found
}

func cancelCause(reason string) error {
	if reason == "" {
		return context.Canceled
	}
	return errors.New(reason)
}

func (c *Control) PopCancelReason() string {
	var reason string
	c.sync(func(st *controlState) {
		reason = st.cancelReason
		st.cancelReason = ""
		st.cancelExecutionID = ""
	})
	return reason
}

func (c *Control) PopSteering() []agentkit.SteerEntry {
	var out []agentkit.SteerEntry
	c.sync(func(st *controlState) {
		if len(st.steering) == 0 {
			return
		}
		out = append([]agentkit.SteerEntry(nil), st.steering...)
		st.steering = nil
	})
	return out
}

// RestoreSteering returns unconsumed input ahead of messages that arrived while
// an agent was attempting delivery to an external runtime.
func (c *Control) RestoreSteering(entries []agentkit.SteerEntry) {
	if len(entries) == 0 {
		return
	}
	c.sync(func(st *controlState) {
		restored := make([]agentkit.SteerEntry, 0, len(entries)+len(st.steering))
		restored = append(restored, entries...)
		st.steering = append(restored, st.steering...)
	})
}

func (c *Control) HasSteering() bool {
	var ok bool
	c.sync(func(st *controlState) {
		ok = len(st.steering) > 0
	})
	return ok
}
