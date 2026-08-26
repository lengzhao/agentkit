package multiplex

import (
	"github.com/lengzhao/agentkit/cap/permission"
)

var _ permission.CapabilityRouter = (*Platform)(nil)

func (m *Platform) PermissionCapabilityFor(id string) permission.Capability {
	p, ok := m.platforms[id]
	if !ok {
		return permission.Capability{Interactive: false}
	}
	if c, ok := p.(permission.Capable); ok {
		return c.PermissionCapability()
	}
	return permission.Capability{Interactive: false}
}
