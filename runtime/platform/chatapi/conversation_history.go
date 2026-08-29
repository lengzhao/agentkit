package chatapi

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// persistConversationHistory mirrors one agent turn into the delivery session file
// for this conversation. Slash commands are handled locally and are not mirrored.
// Runner may collapse agent history to channel/user scope; chat-api keeps
// per-conversation display history like cc-connect Session.History.
func (p *Platform) persistConversationHistory(ctx context.Context, run *runState, answer string) {
	if p.sessionStore == nil || run == nil {
		return
	}
	query := strings.TrimSpace(run.userQuery)
	answer = strings.TrimSpace(answer)
	if query == "" && answer == "" {
		return
	}

	tenantCtx := p.sessionCtx(ctx, run.channelKey, run.conversationID)
	if run.user != "" {
		tenantCtx = context.WithValue(tenantCtx, agentkit.KeyUserID, run.user)
	}
	sessionID := agentkit.SessionID(engineSessionKey(run.channelKey, run.conversationID))
	sess, err := p.sessionStore.Get(tenantCtx, sessionID)
	if err != nil {
		slog.Warn("chat-api: open delivery session for history mirror", "conversation", run.conversationID, "err", err)
		return
	}
	agentID := run.agentID
	if agentID == "" {
		agentID = p.agentID
	}
	if query != "" {
		if err := session.AppendMessage(tenantCtx, sess, agentID, agentkit.EventUserMessage, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: query}},
		}); err != nil {
			slog.Warn("chat-api: mirror user message", "conversation", run.conversationID, "err", err)
			return
		}
	}
	if answer != "" {
		if err := session.AppendMessage(tenantCtx, sess, agentID, agentkit.EventAssistantMessage, agentkit.ModelMessage{
			Role:    "assistant",
			Content: []agentkit.ContentPart{{Type: "text", Text: answer}},
		}); err != nil {
			slog.Warn("chat-api: mirror assistant message", "conversation", run.conversationID, "err", err)
		}
	}
}
