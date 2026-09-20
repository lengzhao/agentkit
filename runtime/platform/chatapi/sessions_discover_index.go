package chatapi

import (
	"context"
)

// syncConversationsFromSessionIndex refreshes the tenant FTS index and lists
// sessions from it. Both sync and list run under the channel workspace scope so
// they hit the same per-tenant index the turn-complete hook maintains.
func (p *Platform) syncConversationsFromSessionIndex(ctx context.Context, channelKey string) error {
	if p.sessionIndex == nil {
		return nil
	}
	tenantCtx := channelWorkspaceCtx(ctx, channelKey)
	if err := p.sessionIndex.SyncSessions(tenantCtx); err != nil {
		return err
	}
	summaries, err := p.sessionIndex.ListSessions(tenantCtx, 200)
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
