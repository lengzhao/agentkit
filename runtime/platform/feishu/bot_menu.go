package feishu

import (
	"context"
	"log/slog"
	"strings"

	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// onBotMenu handles bot custom menu click events. When a menu item's
// event_key starts with "/", it is dispatched as a slash command.
// This allows users to configure menu items in the Feishu developer
// console with event_key set to commands like "/help", "/status", etc.
func (p *Platform) onBotMenu(event *larkapplication.P2BotMenuV6) error {
	if event == nil || event.Event == nil || event.Event.EventKey == nil {
		return nil
	}
	eventKey := *event.Event.EventKey

	userID := ""
	if event.Event.Operator != nil && event.Event.Operator.OperatorId != nil && event.Event.Operator.OperatorId.OpenId != nil {
		userID = *event.Event.Operator.OperatorId.OpenId
	}
	if userID == "" {
		slog.Debug(p.tag()+": bot menu event without user id", "event_key", eventKey)
		return nil
	}

	if !common.AllowList(p.allowFrom, userID) {
		slog.Debug(p.tag()+": menu event from unauthorized user", "user", userID, "event_key", eventKey)
		return nil
	}
	if p.groupOnly {
		slog.Debug(p.tag()+": bot menu skipped (group_only=true)", "user", userID)
		return nil
	}

	slog.Info(p.tag()+": bot menu clicked", "event_key", eventKey, "user", userID)

	content := eventKey
	if !strings.HasPrefix(content, "/") {
		content = "/" + content
	}

	sessionKey := string(rctx.BuildDeliverySessionID(p.platformTag, userID, "", userID))

	p.dispatchInbound(context.Background(), inboundMessage{
		sessionID: agentkit.SessionID(sessionKey),
		content:   content,
		userID:    userID,
		rctx:      replyContext{chatID: userID, chatType: "p2p", sessionKey: sessionKey},
	})
	return nil
}
