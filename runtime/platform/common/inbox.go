package common

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// Inbox bridges async platform events into blocking Receive.
type Inbox struct {
	ch chan agentkit.MessageEvent
}

func NewInbox(buffer int) *Inbox {
	if buffer <= 0 {
		buffer = 32
	}
	return &Inbox{ch: make(chan agentkit.MessageEvent, buffer)}
}

func (i *Inbox) Push(ctx context.Context, event agentkit.MessageEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case i.ch <- event:
		return nil
	}
}

func (i *Inbox) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	select {
	case <-ctx.Done():
		return agentkit.MessageEvent{}, ctx.Err()
	case event := <-i.ch:
		return event, nil
	}
}
