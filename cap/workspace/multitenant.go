package workspace

import "context"

// LocalTenantWalker enumerates isolated local workspace roots (multi-tenant setups).
// Implementations call fn with TurnEnvelope.Workspace set for each tenant.
type LocalTenantWalker interface {
	Service
	WalkLocalTenants(ctx context.Context, fn func(context.Context) error) error
}
