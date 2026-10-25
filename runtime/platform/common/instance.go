package common

import "sync/atomic"

// InstanceIdentity lets aggregators assign a leaf identity before serving traffic.
// Atomic storage also protects native event callbacks during startup.
type InstanceIdentity struct{ instance atomic.Value }

func (i *InstanceIdentity) SetPlatformInstance(id string) { i.instance.Store(id) }
func (i *InstanceIdentity) PlatformInstance() string {
	if id := i.instance.Load(); id != nil {
		return id.(string)
	}
	return ""
}
