package common

import (
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
)

// AgentRoutingConfig optionally pins inbound messages to a Loop agent.
// Empty agentId uses loop.defaultAgent.
type AgentRoutingConfig struct {
	AgentID agentkit.AgentID `json:"agentId"`
	// SessionScope mirrors runner.config.sessionScope for platform-local slash
	// commands (/new active-session mapping). Empty defaults to channel.
	SessionScope string `json:"sessionScope"`
}

func (c AgentRoutingConfig) ResolveAgentID() agentkit.AgentID {
	return agentkit.AgentID(strings.TrimSpace(string(c.AgentID)))
}

func InboundMessage(agentID agentkit.AgentID, sessionID agentkit.SessionID, platformID, userID, text string) agentkit.MessageEvent {
	return WithDeliverySession(agentkit.MessageEvent{
		AgentID:    agentID,
		PlatformID: platformID,
		UserID:     userID,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
	}, platformID, sessionID)
}

func PermissionReplyEvent(agentID agentkit.AgentID, sessionID agentkit.SessionID, platformID, userID string, reply permission.Reply) agentkit.MessageEvent {
	return PermissionReplyEventWithConversation(agentID, sessionID, platformID, userID, "", reply)
}

func PermissionReplyEventWithConversation(agentID agentkit.AgentID, sessionID agentkit.SessionID, platformID, userID, conversation string, reply permission.Reply) agentkit.MessageEvent {
	event := WithDeliverySession(agentkit.MessageEvent{
		AgentID:    agentID,
		PlatformID: platformID,
		UserID:     userID,
		Reply:      rtpermission.MarshalReply(reply),
	}, platformID, sessionID)
	if conversation = strings.TrimSpace(conversation); conversation != "" {
		event.Envelope.Conversation = conversation
	}
	return event
}
