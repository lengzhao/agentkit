package acpremote

import (
	"context"
	"sync"
)

// requestMutex supports cancellation while a command is queued.
type requestMutex struct {
	once sync.Once
	held chan struct{}
}

func (m *requestMutex) Lock(ctx context.Context) error {
	m.once.Do(func() { m.held = make(chan struct{}, 1) })
	select {
	case m.held <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *requestMutex) Unlock() { <-m.held }
