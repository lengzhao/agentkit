package feishu

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
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
	// 单卡回复时，机器人卡片挂在 progressHandle。
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
	return false
}

func (p *Platform) addBotReplyEndReaction(messageID string, endData capsession.TurnEndData, clearProcessingReactionID string) {
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

func (p *Platform) applyTurnEndReactions(sessionID agentkit.SessionID, endData capsession.TurnEndData) {
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

func (p *Platform) addReaction(messageID string) string {
	return p.addReactionWithEmoji(messageID, p.reactionEmoji)
}

func (p *Platform) addReactionWithEmoji(messageID, emojiType string) string {
	if emojiType == "" {
		return ""
	}
	resp, err := p.client.Im.MessageReaction.Create(context.Background(),
		larkim.NewCreateMessageReactionReqBuilder().
			MessageId(messageID).
			Body(larkim.NewCreateMessageReactionReqBodyBuilder().
				ReactionType(&larkim.Emoji{EmojiType: &emojiType}).
				Build()).
			Build())
	if err != nil {
		slog.Debug(p.tag()+": add reaction failed", "error", err)
		return ""
	}
	if !resp.Success() {
		slog.Debug(p.tag()+": add reaction failed", "code", resp.Code, "msg", resp.Msg)
		return ""
	}
	if resp.Data != nil && resp.Data.ReactionId != nil {
		return *resp.Data.ReactionId
	}
	return ""
}

func (p *Platform) removeReaction(messageID, reactionID string) {
	if reactionID == "" || messageID == "" {
		return
	}
	resp, err := p.client.Im.MessageReaction.Delete(context.Background(),
		larkim.NewDeleteMessageReactionReqBuilder().
			MessageId(messageID).
			ReactionId(reactionID).
			Build())
	if err != nil {
		slog.Debug(p.tag()+": remove reaction failed", "error", err)
		return
	}
	if !resp.Success() {
		slog.Debug(p.tag()+": remove reaction failed", "code", resp.Code, "msg", resp.Msg)
	}
}

// StartTyping adds an emoji reaction to the user's message and returns a stop
// function that removes the reaction when processing is complete.
func (p *Platform) StartTyping(ctx context.Context, rctx any) (stop func()) {
	rc, ok := rctx.(replyContext)
	if !ok || rc.messageID == "" {
		return func() {}
	}
	reactionID := p.addReaction(rc.messageID)
	return func() {
		go p.removeReaction(rc.messageID, reactionID)
	}
}

// AddDoneReaction adds a "done" emoji reaction so the user gets a push
// notification when the agent finishes a multi-round turn in quiet mode.
func (p *Platform) AddDoneReaction(rctx any) {
	if p.doneEmoji == "" {
		return
	}
	rc, ok := rctx.(replyContext)
	if !ok || rc.messageID == "" {
		return
	}
	go p.addReactionWithEmoji(rc.messageID, p.doneEmoji)
}
