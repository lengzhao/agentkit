package permission

import (
	"context"

	"github.com/lengzhao/agentkit"
	capspermission "github.com/lengzhao/agentkit/cap/permission"
)

func CapabilityFrom(ctx context.Context) capspermission.Capability {
	if ctrl, ok := ctx.Value(agentkit.KeySessionControl).(interface {
		PermissionCapability() capspermission.Capability
	}); ok && ctrl != nil {
		return ctrl.PermissionCapability()
	}
	return capspermission.Capability{Interactive: false}
}

func BrokerFrom(ctx context.Context) (capspermission.Broker, bool) {
	broker, ok := ctx.Value(agentkit.KeySessionControl).(capspermission.Broker)
	if !ok || broker == nil {
		return nil, false
	}
	return broker, true
}
