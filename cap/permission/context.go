package permission

import (
	"context"

	"github.com/lengzhao/agentkit"
)

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
