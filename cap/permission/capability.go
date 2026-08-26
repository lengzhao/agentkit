package permission

import (
	"context"
	"time"

	"github.com/lengzhao/agentkit"
)

type AnswerScope string

const (
	ScopeAsker  AnswerScope = "asker"
	ScopeAnyone AnswerScope = "anyone"
)

// Capability describes how a platform renders and collects permission replies.
type Capability struct {
	Interactive    bool
	MultiSelect    bool
	DefaultTimeout time.Duration
	AnswerScope    AnswerScope
}

// Capable is implemented by leaf platforms that can render permission requests.
type Capable interface {
	PermissionCapability() Capability
}

// CapabilityRouter is implemented by platforms that aggregate multiple leaf platforms.
type CapabilityRouter interface {
	PermissionCapabilityFor(platformID string) Capability
}

func CapabilityFrom(ctx context.Context) Capability {
	if ctrl, ok := ctx.Value(agentkit.KeySessionControl).(interface {
		PermissionCapability() Capability
	}); ok && ctrl != nil {
		return ctrl.PermissionCapability()
	}
	return Capability{Interactive: false}
}

func BrokerFrom(ctx context.Context) (Broker, bool) {
	broker, ok := ctx.Value(agentkit.KeySessionControl).(Broker)
	if !ok || broker == nil {
		return nil, false
	}
	return broker, true
}
