package headless

import "github.com/lengzhao/agentkit/cap/permission"

func (w *Worker) PermissionCapability() permission.Capability {
	return permission.Capability{Interactive: false}
}

func (t *Timer) PermissionCapability() permission.Capability {
	return permission.Capability{Interactive: false}
}
