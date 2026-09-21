package permission

import (
	"context"

	capspermission "github.com/lengzhao/agentkit/cap/permission"
)

func CapabilityFrom(ctx context.Context) capspermission.Capability {
	return capspermission.CapabilityFrom(ctx)
}

func BrokerFrom(ctx context.Context) (capspermission.Broker, bool) {
	return capspermission.BrokerFrom(ctx)
}
