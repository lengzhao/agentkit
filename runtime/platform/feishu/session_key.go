package feishu

import (
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// makeSessionKey builds the finest-grain delivery SessionID for Feishu events.
func (p *Platform) makeSessionKey(msg *larkim.EventMessage, chatID, userID string) string {
	var thread string
	if msg != nil && ((p.threadIsolation && stringValue(msg.ChatType) == "group") ||
		(p.cfg.DMThreadMode && stringValue(msg.ChatType) == "p2p")) {
		rootID := stringValue(msg.RootId)
		if rootID == "" || (stringValue(msg.ChatType) == "p2p" && stringValue(msg.ThreadId) == "") {
			rootID = stringValue(msg.MessageId)
		}
		thread = rootID
	}
	return string(rctx.BuildDeliverySessionID(p.tag(), chatID, thread, userID))
}

func (p *Platform) sessionKeyFromCardAction(chatID, userID string, value map[string]any) string {
	if value != nil {
		if sessionKey, _ := value["session_key"].(string); sessionKey != "" {
			return sessionKey
		}
	}
	return string(rctx.BuildDeliverySessionID(p.tag(), chatID, "", userID))
}

func (p *Platform) shouldReplyInThread(rc replyContext) bool {
	if rc.messageID == "" {
		return false
	}
	// Empty chatType occurs when reconstructing an explicit topic delivery.
	if p.cfg.DMThreadMode && rc.chatType != "group" {
		return true
	}
	return p.replyInThread && rc.chatType == "group"
}

// shouldUseThreadOrReplyAPI is true when we should call Im.Message.Reply (optionally with ReplyInThread).
func (p *Platform) shouldUseThreadOrReplyAPI(rc replyContext) bool {
	if rc.messageID == "" {
		return false
	}
	return !p.noReplyToTrigger || (p.cfg.DMThreadMode && rc.chatType != "group")
}
