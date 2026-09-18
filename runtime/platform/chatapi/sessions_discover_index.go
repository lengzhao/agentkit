package chatapi

import (
	"context"

	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
)

func (p *Platform) syncConversationsFromSessionIndex(ctx context.Context, channelKey, sessionsDir string) error {
	if p.sessionIndex == nil {
		return nil
	}
	if err := sessstore.SyncSessionIndex(ctx, p.sessionIndex, sessionsDir); err != nil {
		return err
	}
	summaries, err := p.sessionIndex.ListSessions(ctx, 200)
	if err != nil {
		return err
	}
	prefix := sessionFilePrefix(channelKey)
	for _, sum := range summaries {
		if sum.MessageCount <= 0 {
			continue
		}
		convID, ok := conversationIDFromSessionFile(channelKey, sum.SessionID+".jsonl", prefix)
		if !ok {
			continue
		}
		c, err := p.loadConversationFromSession(ctx, channelKey, convID, "")
		if err != nil || c == nil {
			continue
		}
		c.indexLastSeq = sum.LastSeq
		if !sum.LastMod.IsZero() && sum.LastMod.After(c.UpdatedAt) {
			c.UpdatedAt = sum.LastMod
		}
		p.conversations.register(c)
	}
	return nil
}
