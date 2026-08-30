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
type Control struct {
	mu           sync.Mutex
	stepMu       sync.Mutex
	stepCancel   context.CancelFunc
	steering     []agentkit.ModelMessage
	followUps    []agentkit.ModelMessage
	cancelReason string
	capability   permission.Capability
	permissionPending *pendingPermission
}

func NewControl() *Control {
	return &Control{}
}

func (c *Control) setTurnCapability(cap permission.Capability) {
	c.mu.Lock()
	c.capability = cap
	c.mu.Unlock()
}

func (c *Control) PermissionCapability() permission.Capability {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.capability
}

func (c *Control) Steer(_ context.Context, msg agentkit.ModelMessage) error {
	c.mu.Lock()
	c.steering = append(c.steering, msg)
	c.mu.Unlock()
	return nil
}

func (c *Control) FollowUp(_ context.Context, msg agentkit.ModelMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.followUps = append(c.followUps, msg)
	return nil
}

func (c *Control) Cancel(_ context.Context, reason string) error {
	c.mu.Lock()
	c.cancelReason = reason
	c.mu.Unlock()
	c.cancelStep()
	return nil
}

func (c *Control) DrainFollowUps(_ context.Context, mode agentkit.FollowUpMode) ([]agentkit.ModelMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.followUps) == 0 {
		return nil, nil
	}
	switch mode {
	case agentkit.FollowUpAll:
		out := append([]agentkit.ModelMessage(nil), c.followUps...)
		c.followUps = nil
		return out, nil
	default:
		msg := c.followUps[0]
		c.followUps = c.followUps[1:]
		return []agentkit.ModelMessage{msg}, nil
	}
}

func (c *Control) ClearTurnCancel() {
	c.mu.Lock()
	c.cancelReason = ""
	c.mu.Unlock()
}

func (c *Control) BeginStep(parent context.Context) (context.Context, func()) {
	stepCtx, cancel := context.WithCancel(parent)
	c.setStepCancel(cancel)
	return stepCtx, func() {
		cancel()
		c.clearStepCancel()
	}
}

func (c *Control) PopCancelReason() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	reason := c.cancelReason
	c.cancelReason = ""
	return reason
}

func (c *Control) PopSteering() []agentkit.ModelMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.steering) == 0 {
		return nil
	}
	out := append([]agentkit.ModelMessage(nil), c.steering...)
	c.steering = nil
	return out
}

func (c *Control) HasSteering() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.steering) > 0
}

func (c *Control) setStepCancel(cancel context.CancelFunc) {
	c.stepMu.Lock()
	c.stepCancel = cancel
	c.stepMu.Unlock()
}

func (c *Control) clearStepCancel() {
	c.setStepCancel(nil)
}

func (c *Control) cancelStep() {
	c.stepMu.Lock()
	cancel := c.stepCancel
	c.stepMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
