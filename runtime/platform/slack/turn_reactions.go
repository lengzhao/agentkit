package slack

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

type turnReactionBatch struct {
	mu         sync.Mutex
	active     bool
	deliveries []delivery
}

func resolveSlackReactionEmoji(raw, defaultVal string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultVal
	}
	if raw == "none" {
		return ""
	}
	return raw
}

func parseTurnEndData(event agentkit.OutboundEvent) capsession.TurnEndData {
	var data capsession.TurnEndData
	if len(event.Data) == 0 {
		return data
	}
	_ = json.Unmarshal(event.Data, &data)
	return data
}

func (p *Platform) turnReactionBatchFor(sessionID agentkit.SessionID) *turnReactionBatch {
	v, _ := p.turnReactions.LoadOrStore(sessionID, &turnReactionBatch{})
	return v.(*turnReactionBatch)
}

func (p *Platform) onTurnStartReactions(sessionID agentkit.SessionID) {
	d, ok := p.deliveryForSession(sessionID)
	if ok && d.msgTS != "" {
		p.turnTriggers.Store(sessionID, d)
	}
	p.beginTurnReactions(sessionID, d)
}

func (p *Platform) beginTurnReactions(sessionID agentkit.SessionID, d delivery) {
	batch := p.turnReactionBatchFor(sessionID)
	batch.mu.Lock()
	defer batch.mu.Unlock()
	batch.active = true
	batch.deliveries = nil
	if d.msgTS != "" {
		batch.deliveries = append(batch.deliveries, d)
	}
}

func (p *Platform) appendTurnReactionIfActive(sessionID agentkit.SessionID, d delivery) {
	if d.msgTS == "" {
		return
	}
	batch := p.turnReactionBatchFor(sessionID)
	batch.mu.Lock()
	defer batch.mu.Unlock()
	if !batch.active {
		return
	}
	for _, existing := range batch.deliveries {
		if existing.msgTS == d.msgTS && existing.channel == d.channel {
			return
		}
	}
	batch.deliveries = append(batch.deliveries, d)
}

func (p *Platform) finishTurnReactions(sessionID agentkit.SessionID) []delivery {
	batch := p.turnReactionBatchFor(sessionID)
	batch.mu.Lock()
	defer batch.mu.Unlock()
	batch.active = false
	out := append([]delivery(nil), batch.deliveries...)
	batch.deliveries = nil
	return out
}

func (p *Platform) deliveryForSession(sessionID agentkit.SessionID) (delivery, bool) {
	raw, ok := p.deliveries.Load(sessionID)
	if !ok {
		return delivery{}, false
	}
	return raw.(delivery), true
}

func (p *Platform) applyTurnEndReactions(ctx context.Context, sessionID agentkit.SessionID, endData capsession.TurnEndData) {
	deliveries := p.finishTurnReactions(sessionID)
	trigger, hasTrigger := p.turnTriggers.LoadAndDelete(sessionID)
	if len(deliveries) == 0 {
		if hasTrigger {
			deliveries = []delivery{trigger.(delivery)}
		} else if d, ok := p.deliveryForSession(sessionID); ok && d.msgTS != "" {
			deliveries = []delivery{d}
		}
	}
	for _, d := range deliveries {
		p.applyTurnEndReaction(ctx, d, endData)
	}
}

func (p *Platform) applyTurnEndReaction(ctx context.Context, d delivery, endData capsession.TurnEndData) {
	var emoji string
	switch {
	case endData.Cancelled:
		emoji = p.cancelledEmoji
	case endData.Failed:
		emoji = p.errorEmoji
	default:
		emoji = p.doneEmoji
	}
	go func() {
		bg := context.WithoutCancel(ctx)
		p.removeReaction(bg, d, reactionReceived)
		if emoji != "" {
			p.addReaction(bg, d, emoji)
		}
	}()
}
