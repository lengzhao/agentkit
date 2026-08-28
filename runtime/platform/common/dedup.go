package common

import (
	"sync"
	"time"
)

const dedupTTL = 60 * time.Second

// ProcessStartTime is set once at process startup. Platforms discard messages
// created before this time to avoid replay after restart.
var ProcessStartTime = time.Now()

// MessageDedup tracks recently seen message IDs to prevent duplicate processing.
type MessageDedup struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// IsDuplicate returns true if msgID was already seen within the TTL window.
func (d *MessageDedup) IsDuplicate(msgID string) bool {
	if msgID == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = make(map[string]time.Time)
	}
	now := time.Now()
	for k, t := range d.seen {
		if now.Sub(t) > dedupTTL {
			delete(d.seen, k)
		}
	}
	if _, ok := d.seen[msgID]; ok {
		return true
	}
	d.seen[msgID] = now
	return false
}

// IsOldMessage returns true if msgTime is before process start (with grace).
func IsOldMessage(msgTime time.Time) bool {
	return msgTime.Before(ProcessStartTime.Add(-2 * time.Second))
}
