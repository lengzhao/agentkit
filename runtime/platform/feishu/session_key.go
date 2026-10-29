package feishu

import (
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// makeSessionKey builds the finest-grain delivery SessionID for Feishu events.
func (p *Platform) makeSessionKey(msg *larkim.EventMessage, chatID, userID string) string {
	var thread string
	if msg != nil && ((p.threadIsolation && stringValue(msg.ChatType) == "group") ||
		(stringValue(msg.ChatType) == "p2p" && (stringValue(msg.ThreadId) != "" || p.dmThreadEnabled(userID)))) {
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
	if rc.explicitThread {
		return true
	}
	if rc.chatType == "p2p" {
		return isThreadSessionKey(rc.sessionKey)
	}
	if !p.replyInThread {
		return false
	}
	// Group topic behavior remains controlled by replyInThread.
	return rc.chatType == "group"
}

// shouldUseThreadOrReplyAPI is true when we should call Im.Message.Reply (optionally with ReplyInThread).
func (p *Platform) shouldUseThreadOrReplyAPI(rc replyContext) bool {
	if rc.messageID == "" {
		return false
	}
	return p.mustStayInThread(rc) || !p.noReplyToTrigger
}

// shouldFetchQuotedMessage keeps an explicitly quoted message on a new DM
// topic, while avoiding duplicate context on an existing topic's follow-ups.
func (p *Platform) shouldFetchQuotedMessage(rc replyContext, parentID string) bool {
	if parentID == "" {
		return false
	}
	parts := rctx.ParseDelivery(agentkit.SessionID(rc.sessionKey), "")
	if rc.chatType == "p2p" {
		return parts.Thread == "" || parts.Thread == rc.messageID
	}
	return !p.threadIsolation || parts.Thread == ""
}

// mustStayInThread forbids fallback to a standalone message for DM topics
// and reconstructed deliveries that explicitly name a topic.
func (p *Platform) mustStayInThread(rc replyContext) bool {
	return rc.explicitThread || (rc.chatType == "p2p" && p.shouldReplyInThread(rc))
}
