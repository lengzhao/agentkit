package cli

import "context"

// beginTurnWait marks a turn in flight. Receive blocks on the main prompt until
// endTurnWait runs, except while a permission prompt is active.
func (p *Platform) beginTurnWait() {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()
	if p.turnDone == nil {
		p.turnDone = make(chan struct{})
	}
}

func (p *Platform) endTurnWait() {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()
	if p.turnDone != nil {
		close(p.turnDone)
		p.turnDone = nil
	}
}

func (p *Platform) waitTurnIdle(ctx context.Context) error {
	p.turnMu.Lock()
	ch := p.turnDone
	p.turnMu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
