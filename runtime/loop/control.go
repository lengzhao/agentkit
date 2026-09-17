package loop

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

// Control holds steer / follow-up queues and step-cancel state for one session.
// Loop owns one Control per SessionID; Agent reads step-level hooks via
// ctx.Value(agentkit.KeySessionControl).
//
// All mutable state is owned by a single goroutine; callers synchronize via ch.
type Control struct {
	ch chan func(*controlState)
}

type controlState struct {
	stepCancel        context.CancelFunc
	steering          []agentkit.ModelMessage
	followUps         []agentkit.ModelMessage
	cancelReason      string
	capability        permission.Capability
	permissionPending *pendingPermission
}

func NewControl() *Control {
	c := &Control{ch: make(chan func(*controlState))}
	go c.loop()
	return c
}

func (c *Control) loop() {
	var st controlState
	for fn := range c.ch {
		fn(&st)
	}
	// Drain any pending permission waiters so CancelAllInFlight callers do not
	// block forever after the owner goroutine retires.
	if st.permissionPending != nil {
		st.permissionPending.finish()
	}
	if st.stepCancel != nil {
		st.stepCancel()
	}
}

// Stop retires the owner goroutine. Safe to call once; subsequent sync calls
// will panic on a closed channel. Loop calls this on shutdown for every session.
func (c *Control) Stop() {
	close(c.ch)
}

func (c *Control) sync(fn func(*controlState)) {
	done := make(chan struct{})
	c.ch <- func(st *controlState) {
		fn(st)
		close(done)
	}
	<-done
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
		st.steering = append(st.steering, msg)
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
	var cancel context.CancelFunc
	c.sync(func(st *controlState) {
		st.cancelReason = reason
		cancel = st.stepCancel
	})
	if cancel != nil {
		cancel()
	}
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
	})
}

func (c *Control) BeginStep(parent context.Context) (context.Context, func()) {
	stepCtx, cancel := context.WithCancel(parent)
	c.sync(func(st *controlState) {
		st.stepCancel = cancel
	})
	return stepCtx, func() {
		cancel()
		c.sync(func(st *controlState) {
			st.stepCancel = nil
		})
	}
}

func (c *Control) PopCancelReason() string {
	var reason string
	c.sync(func(st *controlState) {
		reason = st.cancelReason
		st.cancelReason = ""
	})
	return reason
}

func (c *Control) PopSteering() []agentkit.ModelMessage {
	var out []agentkit.ModelMessage
	c.sync(func(st *controlState) {
		if len(st.steering) == 0 {
			return
		}
		out = append([]agentkit.ModelMessage(nil), st.steering...)
		st.steering = nil
	})
	return out
}

func (c *Control) HasSteering() bool {
	var ok bool
	c.sync(func(st *controlState) {
		ok = len(st.steering) > 0
	})
	return ok
}
