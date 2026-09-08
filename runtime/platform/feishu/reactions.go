package feishu

import (
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

type turnReactionBatch struct {
	mu     sync.Mutex
	active bool
	rcs    []replyContext
}

func (p *Platform) turnReactionBatchFor(sessionID agentkit.SessionID) *turnReactionBatch {
	v, _ := p.turnReactions.LoadOrStore(sessionID, &turnReactionBatch{})
	return v.(*turnReactionBatch)
}

func (p *Platform) onTurnStartReactions(sessionID agentkit.SessionID) {
	rc, ok := p.deliveryFor(sessionID)
	if ok && rc.messageID != "" {
		p.turnTriggers.Store(sessionID, rc)
	}
	p.beginTurnReactions(sessionID, rc)
}

func (p *Platform) beginTurnReactions(sessionID agentkit.SessionID, rc replyContext) {
	batch := p.turnReactionBatchFor(sessionID)
	batch.mu.Lock()
	defer batch.mu.Unlock()
	batch.active = true
	batch.rcs = nil
	if rc.messageID != "" {
		batch.rcs = append(batch.rcs, rc)
	}
}

func (p *Platform) appendTurnReactionIfActive(sessionID agentkit.SessionID, rc replyContext) {
	if rc.messageID == "" {
		return
	}
	batch := p.turnReactionBatchFor(sessionID)
	batch.mu.Lock()
	defer batch.mu.Unlock()
	if !batch.active {
		return
	}
	for _, existing := range batch.rcs {
		if existing.messageID == rc.messageID {
			return
		}
	}
	batch.rcs = append(batch.rcs, rc)
}

func (p *Platform) finishTurnReactions(sessionID agentkit.SessionID) []replyContext {
	batch := p.turnReactionBatchFor(sessionID)
	batch.mu.Lock()
	defer batch.mu.Unlock()
	batch.active = false
	out := append([]replyContext(nil), batch.rcs...)
	batch.rcs = nil
	return out
}

func (p *Platform) applyTurnEndReactions(sessionID agentkit.SessionID, endData session.TurnEndData) {
	rcs := p.finishTurnReactions(sessionID)
	p.turnTriggers.LoadAndDelete(sessionID)
	if len(rcs) == 0 {
		if triggerRC, ok := p.turnTriggerFor(sessionID); ok {
			rcs = []replyContext{triggerRC}
		}
	}
	for _, rc := range rcs {
		switch {
		case endData.Cancelled:
			p.addCancelledReaction(rc)
		case endData.Failed:
			p.addErrorReaction(rc)
		default:
			p.addDoneReaction(rc)
		}
	}
}
