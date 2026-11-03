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
	duplicate, _ := d.Admit(msgID, func() error { return nil })
	return duplicate
}

// Admit atomically records an ID only after nonblocking queue admission succeeds.
// A full/stopped executor can therefore return an SDK error without poisoning
// the dedup window. accept must not block or call back into this MessageDedup.
func (d *MessageDedup) Admit(msgID string, accept func() error) (bool, error) {
	if msgID == "" {
		return false, accept()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = make(map[string]time.Time)
	}
	now := time.Now()
	for key, at := range d.seen {
		if now.Sub(at) > dedupTTL {
			delete(d.seen, key)
		}
	}
	if _, ok := d.seen[msgID]; ok {
		return true, nil
	}
	if err := accept(); err != nil {
		return false, err
	}
	d.seen[msgID] = now
	return false, nil
}

// IsOldMessage returns true if msgTime is before process start (with grace).
func IsOldMessage(msgTime time.Time) bool {
	return msgTime.Before(ProcessStartTime.Add(-2 * time.Second))
}
