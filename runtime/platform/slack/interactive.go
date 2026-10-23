package slack

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

func (p *Platform) handleInteractive(evt socketmode.Event) {
	callback, ok := evt.Data.(slack.InteractionCallback)
	if !ok {
		return
	}
	if evt.Request != nil && p.socket != nil {
		p.socket.Ack(*evt.Request)
	}
	if callback.Type != slack.InteractionTypeBlockActions && callback.Type != slack.InteractionTypeViewSubmission {
		return
	}
	slog.Info("slack: interaction callback received", "type", callback.Type, "user", callback.User.ID)
	if !common.AllowList(p.cfg.AllowFrom, callback.User.ID) {
		slog.Info("slack: interaction permission filtered", "user", callback.User.ID)
		return
	}
	if callback.Type == slack.InteractionTypeViewSubmission {
		var raw json.RawMessage
		if evt.Request != nil {
			raw = evt.Request.Payload
		}
		p.handleViewSubmission(callback, raw)
		return
	}
	for _, action := range callback.ActionCallback.BlockActions {
		var raw json.RawMessage
		if evt.Request != nil {
			raw = evt.Request.Payload
		}
		p.handleBlockActionPayload(callback, action, raw)
	}
}

func (p *Platform) handleBlockAction(callback slack.InteractionCallback, action *slack.BlockAction) {
	p.handleBlockActionPayload(callback, action, nil)
}
func (p *Platform) handleBlockActionPayload(callback slack.InteractionCallback, action *slack.BlockAction, payload json.RawMessage) {
	if action == nil {
		return
	}
	if callback.Channel.ID == "" {
		callback.Channel.ID = callback.Container.ChannelID
	}
	viewSession, viewThread := "", ""
	if callback.View.ID != "" {
		var channel string
		viewSession, channel, viewThread = p.slackViewRoute(callback)
		callback.Channel.ID = channel
	}
	rawValue := slackActionValue(*action)
	actionVal, sessionKey, extra := decodeActionValue(rawValue)
	if sessionKey == "" && viewSession != "" {
		sessionKey = viewSession
	}
	actualThread := threadTSFromInteractive(callback)
	if actualThread == "" {
		actualThread = viewThread
	}
	if sessionKey != "" {
		parts := rctx.ParseDelivery(agentkit.SessionID(sessionKey), "")
		if !parts.Routable || parts.Platform != "slack" || parts.Channel != callback.Channel.ID || (parts.User != "" && parts.User != callback.User.ID) || (actualThread != "" && parts.Thread != "" && parts.Thread != actualThread) {
			sessionKey = ""
		}
	}
	if sessionKey == "" {
		sessionKey = p.buildSessionKey(callback.Channel.ID, callback.User.ID, actualThread)
	}

	channelID := callback.Channel.ID
	messageTS := callback.Message.Timestamp
	if messageTS == "" {
		messageTS = callback.MessageTs
		if messageTS == "" {
			messageTS = callback.Container.MessageTs
		}
	}
	threadTS := callback.Message.ThreadTimestamp
	if threadTS == "" {
		threadTS = callback.Container.ThreadTs
	}

	if threadTS == "" {
		threadTS = viewThread
	}
	if threadTS == "" {
		parts := rctx.ParseDelivery(agentkit.SessionID(sessionKey), "")
		threadTS = parts.Thread
	}
	if callback.View.ID != "" {
		messageTS = ""
	}

	if !common.AllowList(p.cfg.AllowFrom, callback.User.ID) {
		slog.Info("slack: interaction permission filtered", "user", callback.User.ID)
		return
	}
	// Snapshot SDK-owned maps before returning control to the socket event loop.
	snapshot := slackInteractionSnapshot(callback, action, sessionKey, channelID, messageTS, threadTS, actionVal, payload)
	complete, duplicate := p.interactionDedup.Begin(common.InteractionEventKey(snapshot))
	if duplicate {
		slog.Info("slack: duplicate interaction ignored", "message_id", messageTS)
		return
	}

	go func() {
		var processingErr error
		defer func() { complete(processingErr) }()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if !(snapshot.InteractionID != "" && channelID == snapshot.UserID) && !p.channelAllowed(channelID, inferSlackChannelType(channelID)) {
			slog.Info("slack: interaction permission filtered", "channel", channelID)
			return
		}
		if reply, ok := common.PermissionReplyFromAction(actionVal, snapshot.UserID, extra); ok {
			slog.Info("slack: permission interaction handled", "message_id", messageTS)
			if messageTS != "" {
				p.trackCardMessage(sessionKey, channelID, messageTS, threadTS)
			}
			processingErr = p.pushPermissionReply(ctx, sessionKey, reply, extra)
			if processingErr != nil {
				slog.Error("slack: permission delivery failed", "message_id", messageTS, "error", processingErr)
				return
			}
			if p.client != nil {
				ref := cardMessageRef{channel: channelID, ts: messageTS, threadTS: threadTS}
				if err := p.updateCardMessage(ctx, ref, common.ConfirmedCardFromReply(reply, extra), sessionKey); err != nil {
					slog.Warn("slack: interaction card update failed", "error", err)
				}
			}
			return
		}
		if strings.HasPrefix(actionVal, "perm:") {
			slog.Warn("slack: malformed permission interaction ignored", "message_id", messageTS)
			return
		}
		processingErr = p.processBlockInteraction(ctx, snapshot)
	}()
}

func threadTSFromInteractive(callback slack.InteractionCallback) string {
	if callback.Message.ThreadTimestamp != "" {
		return callback.Message.ThreadTimestamp
	}
	return callback.Container.ThreadTs
}

func (p *Platform) pushPermissionReply(ctx context.Context, sessionKey string, reply permission.Reply, extra map[string]string) error {
	sessionID := agentkit.SessionID(sessionKey)
	return p.inbox.Push(ctx, common.PermissionReplyEventWithConversation(
		p.agentID,
		sessionID,
		"slack",
		reply.UserID,
		common.PermissionConversationFromExtra(extra),
		reply,
	))
}
