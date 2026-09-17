package feishu

import (
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func (p *Platform) clearCardProcessingReaction(messageID, reactionID string) {
	if strings.TrimSpace(messageID) == "" || strings.TrimSpace(reactionID) == "" {
		return
	}
	p.removeReaction(messageID, reactionID)
}

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

func botReplyMessageID(st *streamState) string {
	// progressStyle:card 单卡回复时，机器人卡片挂在 progressHandle。
	if h, ok := st.progressHandle.(*feishuPreviewHandle); ok && h != nil {
		if id := strings.TrimSpace(h.messageID); id != "" {
			return id
		}
	}
	for i := len(st.cards) - 1; i >= 0; i-- {
		if st.cards[i].Kind != streamCardBody {
			continue
		}
		if h, ok := st.cards[i].Handle.(*feishuPreviewHandle); ok && h != nil {
			if id := strings.TrimSpace(h.messageID); id != "" {
				return id
			}
		}
	}
	if h, ok := st.progressHandle.(*feishuPreviewHandle); ok && h != nil {
		return strings.TrimSpace(h.messageID)
	}
	return ""
}

// useBotReplyReactionEmojis reports whether to add done/cancel/error emoji on the bot reply card message.
// Rich stream cards already show ☑️ 用时 (etc.) in the card body.
func (p *Platform) useBotReplyReactionEmojis() bool {
	return !p.useRichStream()
}

func (p *Platform) addBotReplyEndReaction(messageID string, endData session.TurnEndData, clearProcessingReactionID string) {
	if !p.useBotReplyReactionEmojis() {
		return
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return
	}
	if clearProcessingReactionID != "" {
		p.clearCardProcessingReaction(messageID, clearProcessingReactionID)
	}
	switch {
	case endData.Cancelled:
		if p.cancelledEmoji != "" {
			go p.addReactionWithEmoji(messageID, p.cancelledEmoji)
		}
	case endData.Failed:
		if p.errorEmoji != "" {
			go p.addReactionWithEmoji(messageID, p.errorEmoji)
		}
	default:
		if p.doneEmoji != "" {
			go p.addReactionWithEmoji(messageID, p.doneEmoji)
		}
	}
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
