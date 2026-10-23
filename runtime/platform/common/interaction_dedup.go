package common

import (
	"sync"
	"time"
)

// InteractionDedup reserves callbacks while work is in flight. Failed delivery
// releases the reservation; only completed work enters the replay window.
type InteractionDedup struct {
	mu   sync.Mutex
	seen map[string]*interactionReceipt
}
type interactionReceipt struct{ completed time.Time }

// Begin returns an idempotent completion callback and whether this event is
// already pending/completed. Call complete with the actual processing result.
func (d *InteractionDedup) Begin(key string) (complete func(error), duplicate bool) {
	d.mu.Lock()
	if d.seen == nil {
		d.seen = make(map[string]*interactionReceipt)
	}
	now := time.Now()
	for k, r := range d.seen {
		if !r.completed.IsZero() && now.Sub(r.completed) > dedupTTL {
			delete(d.seen, k)
		}
	}
	if _, ok := d.seen[key]; ok {
		d.mu.Unlock()
		return func(error) {}, true
	}
	receipt := &interactionReceipt{}
	d.seen[key] = receipt
	d.mu.Unlock()
	var once sync.Once
	return func(err error) {
		once.Do(func() {
			d.mu.Lock()
			defer d.mu.Unlock()
			if err != nil {
				delete(d.seen, key)
			} else {
				receipt.completed = time.Now()
			}
		})
	}, false
}
