package feishu

import (
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// cardProcessingReactionTypes alternate on the unified CardKit reply card every progressHeartbeatInterval.
var cardProcessingReactionTypes = [2]string{"Typing", "OneSecond"}

func cardProcessingReactionEmoji(pulseIndex int) string {
	if pulseIndex < 0 {
		pulseIndex = 0
	}
	return cardProcessingReactionTypes[pulseIndex%len(cardProcessingReactionTypes)]
}

func shouldReactionHeartbeat(st *streamState) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.startedAt.IsZero() {
		return false
	}
	switch st.status {
	case cardStatusDone, cardStatusCancelled, cardStatusError:
		return false
	}
	return true
}

func shouldCardReactionHeartbeat(st *streamState) bool {
	if !shouldReactionHeartbeat(st) {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return botReplyMessageID(st) != ""
}

func (p *Platform) swapMessageReaction(messageID, oldReactionID, emojiType string) string {
	if strings.TrimSpace(messageID) == "" || strings.TrimSpace(emojiType) == "" {
		return oldReactionID
	}
	if oldReactionID != "" {
		p.removeReaction(messageID, oldReactionID)
	}
	return p.addReactionWithEmoji(messageID, emojiType)
}

func (p *Platform) attachCardProcessingReaction(sessionID agentkit.SessionID) {
	if !p.useUnifiedStreamCard() {
		return
	}
	st := p.streamState(sessionID)
	st.mu.Lock()
	if st.cardReactionID != "" {
		st.mu.Unlock()
		return
	}
	msgID := botReplyMessageID(st)
	st.mu.Unlock()
	if msgID == "" {
		return
	}
	id := p.addReactionWithEmoji(msgID, cardProcessingReactionEmoji(0))
	if id == "" {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.cardReactionID != "" {
		p.removeReaction(msgID, id)
		return
	}
	st.cardReactionID = id
	st.heartbeatDigitIndex = 1
}

func (p *Platform) rotateCardReplyReaction(sessionID agentkit.SessionID) {
	if !p.useUnifiedStreamCard() {
		return
	}
	raw, ok := p.streams.Load(sessionID)
	if !ok {
		return
	}
	st := raw.(*streamState)
	st.mu.Lock()
	msgID := botReplyMessageID(st)
	oldID := st.cardReactionID
	if msgID == "" {
		st.mu.Unlock()
		return
	}
	if oldID == "" {
		st.mu.Unlock()
		p.attachCardProcessingReaction(sessionID)
		return
	}
	idx := st.heartbeatDigitIndex % len(cardProcessingReactionTypes)
	st.heartbeatDigitIndex++
	st.mu.Unlock()

	newID := p.swapMessageReaction(msgID, oldID, cardProcessingReactionEmoji(idx))
	if newID == "" {
		st.mu.Lock()
		st.cardReactionID = ""
		st.mu.Unlock()
		return
	}
	st.mu.Lock()
	st.cardReactionID = newID
	st.mu.Unlock()
}

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
	if st.unifiedTextFallback {
		if h, ok := st.textFallbackHandle.(*feishuPreviewHandle); ok && h != nil {
			if id := strings.TrimSpace(h.messageID); id != "" {
				return id
			}
		}
	}
	if h, ok := st.cardHandle.(*feishuPreviewHandle); ok && h != nil {
		if id := strings.TrimSpace(h.messageID); id != "" {
			return id
		}
	}
	if h, ok := st.bodyHandle.(*feishuPreviewHandle); ok && h != nil {
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

func (p *Platform) addBotReplyEndReaction(messageID string, endData session.TurnEndData, clearProcessingReactionID string) {
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
