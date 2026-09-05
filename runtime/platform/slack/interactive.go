package slack

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

func (p *Platform) handleInteractive(evt socketmode.Event) {
	callback, ok := evt.Data.(slack.InteractionCallback)
	if !ok {
		return
	}
	if evt.Request != nil {
		p.socket.Ack(*evt.Request)
	}
	if callback.Type != slack.InteractionTypeBlockActions {
		return
	}
	if !common.AllowList(p.cfg.AllowFrom, callback.User.ID) {
		return
	}
	for _, action := range callback.ActionCallback.BlockActions {
		p.handleBlockAction(callback, action)
	}
}

func (p *Platform) handleBlockAction(callback slack.InteractionCallback, action *slack.BlockAction) {
	if action == nil {
		return
	}
	actionVal := strings.TrimSpace(action.Value)
	if actionVal == "" {
		return
	}
	actionVal, sessionKey, extra := decodeActionValue(actionVal)
	if sessionKey == "" {
		sessionKey = p.buildSessionKey(callback.Channel.ID, callback.User.ID, threadTSFromInteractive(callback))
	}
	reply, ok := common.PermissionReplyFromAction(actionVal, callback.User.ID, extra)
	if !ok {
		return
	}

	channelID := callback.Channel.ID
	messageTS := callback.Message.Timestamp
	if messageTS == "" {
		messageTS = callback.MessageTs
	}
	threadTS := callback.Message.ThreadTimestamp
	if threadTS == "" {
		threadTS = callback.Container.ThreadTs
	}
	if messageTS != "" {
		p.trackCardMessage(sessionKey, channelID, messageTS, threadTS)
	}

	p.pushPermissionReply(context.Background(), sessionKey, reply, extra)

	confirmed := common.ConfirmedCardFromReply(reply, extra)
	ref := cardMessageRef{channel: channelID, ts: messageTS, threadTS: threadTS}
	if err := p.updateCardMessage(context.Background(), ref, confirmed, sessionKey); err != nil {
		slog.Warn("slack: interaction card update failed", "err", err)
	}
}

func threadTSFromInteractive(callback slack.InteractionCallback) string {
	if callback.Message.ThreadTimestamp != "" {
		return callback.Message.ThreadTimestamp
	}
	return callback.Container.ThreadTs
}

func (p *Platform) pushPermissionReply(ctx context.Context, sessionKey string, reply permission.Reply, extra map[string]string) {
	sessionID := agentkit.SessionID(sessionKey)
	_ = p.inbox.Push(ctx, common.PermissionReplyEventWithConversation(
		p.agentID,
		sessionID,
		"slack",
		reply.UserID,
		common.PermissionConversationFromExtra(extra),
		reply,
	))
}
