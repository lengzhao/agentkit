package loop

import (
	"context"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
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
	stepCancel        context.CancelFunc
	steering          []agentkit.ModelMessage
	followUps         []agentkit.ModelMessage
	cancelReason      string
	capability        permission.Capability
	permissionPending *pendingPermission
	// permissionAllowAll is set when the user replies "allow_all" (全部允许):
	// subsequent KindAllowDeny requests in this session auto-resolve to allow.
	permissionAllowAll bool
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
		if st.stepCancel != nil {
			st.stepCancel()
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
	if !c.sync(func(st *controlState) {
		st.stepCancel = cancel
	}) {
		cancel()
	}
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

// RestoreSteering returns unconsumed input ahead of messages that arrived while
// an agent was attempting delivery to an external runtime.
func (c *Control) RestoreSteering(messages []agentkit.ModelMessage) {
	if len(messages) == 0 {
		return
	}
	c.sync(func(st *controlState) {
		restored := make([]agentkit.ModelMessage, 0, len(messages)+len(st.steering))
		restored = append(restored, messages...)
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
