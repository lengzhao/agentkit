package slack

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

// trackedReaction is one message the running turn marks, together with the
// emoji currently applied to it. turn/end removes exactly that emoji. Feishu
// tracks the reaction id returned by the API; Slack has no id, so the emoji
// name itself is the swappable state.
type trackedReaction struct {
	delivery delivery
	reaction string
}

type turnReactionBatch struct {
	mu      sync.Mutex
	active  bool
	tracked []trackedReaction
}

// sameMessage reports whether two deliveries point at the same chat message.
func sameMessage(a, b delivery) bool {
	return a.msgTS != "" && a.msgTS == b.msgTS && a.channel == b.channel
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
	batch.tracked = nil
	if d.msgTS != "" {
		batch.tracked = append(batch.tracked, trackedReaction{delivery: d, reaction: reactionReceived})
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
	for _, existing := range batch.tracked {
		if sameMessage(existing.delivery, d) {
			return
		}
	}
	batch.tracked = append(batch.tracked, trackedReaction{delivery: d, reaction: reactionReceived})
}

// reactionFor reports the emoji currently tracked for message d while the batch
// is active, so a swap can retire the exact reaction it replaces.
func (b *turnReactionBatch) reactionFor(d delivery) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.active {
		return "", false
	}
	for i := range b.tracked {
		if sameMessage(b.tracked[i].delivery, d) {
			return b.tracked[i].reaction, true
		}
	}
	return "", false
}

// swapReaction records that message d now carries name instead of the
// processing reaction, so turn/end retires the right emoji. Returns false when
// the message is not part of the active batch.
func (b *turnReactionBatch) swapReaction(d delivery, name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.active {
		return false
	}
	for i := range b.tracked {
		if sameMessage(b.tracked[i].delivery, d) {
			b.tracked[i].reaction = name
			return true
		}
	}
	return false
}

func (p *Platform) finishTurnReactions(sessionID agentkit.SessionID) []trackedReaction {
	batch := p.turnReactionBatchFor(sessionID)
	batch.mu.Lock()
	defer batch.mu.Unlock()
	batch.active = false
	out := append([]trackedReaction(nil), batch.tracked...)
	batch.tracked = nil
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
	tracked := p.finishTurnReactions(sessionID)
	trigger, hasTrigger := p.turnTriggers.LoadAndDelete(sessionID)
	if len(tracked) == 0 {
		switch {
		case hasTrigger:
			tracked = []trackedReaction{{delivery: trigger.(delivery), reaction: reactionReceived}}
		default:
			if d, ok := p.deliveryForSession(sessionID); ok && d.msgTS != "" {
				tracked = []trackedReaction{{delivery: d, reaction: reactionReceived}}
			}
		}
	}
	for _, t := range tracked {
		p.applyTurnEndReaction(ctx, t, endData)
	}
}

func (p *Platform) applyTurnEndReaction(ctx context.Context, t trackedReaction, endData capsession.TurnEndData) {
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
		// t.reaction is whatever is on the message now: eyes, or steerEmoji if
		// the message was folded into this turn mid-flight.
		p.removeReaction(bg, t.delivery, t.reaction)
		if t.reaction != reactionReceived {
			p.removeReaction(bg, t.delivery, reactionReceived)
		}
		if emoji != "" {
			p.addReaction(bg, t.delivery, emoji)
		}
	}()
}
