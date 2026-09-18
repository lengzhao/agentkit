package feishu

import (
	"github.com/lengzhao/agentkit/runtime/rctx"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// makeSessionKey builds the finest-grain delivery SessionID for Feishu events.
func (p *Platform) makeSessionKey(msg *larkim.EventMessage, chatID, userID string) string {
	var thread string
	if p.threadIsolation && msg != nil && stringValue(msg.ChatType) == "group" {
		rootID := stringValue(msg.RootId)
		if rootID == "" {
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
	if rc.messageID == "" || !p.replyInThread {
		return false
	}
	// Only group chats support Feishu topic threads; p2p stays flat.
	return rc.chatType == "group"
}

// shouldUseThreadOrReplyAPI is true when we should call Im.Message.Reply (optionally with ReplyInThread).
func (p *Platform) shouldUseThreadOrReplyAPI(rc replyContext) bool {
	if rc.messageID == "" {
		return false
	}
	return !p.noReplyToTrigger
}
