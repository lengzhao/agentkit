package common

import (
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

// AgentRoutingConfig optionally pins inbound messages to a Loop agent.
// Empty agentId uses loop.defaultAgent.
type AgentRoutingConfig struct {
	AgentID agentkit.AgentID `json:"agentId"`
}

func (c AgentRoutingConfig) ResolveAgentID() agentkit.AgentID {
	return agentkit.AgentID(strings.TrimSpace(string(c.AgentID)))
}

func InboundMessage(agentID agentkit.AgentID, sessionID agentkit.SessionID, platformID, userID, text string) agentkit.MessageEvent {
	return agentkit.MessageEvent{
		SessionID:  sessionID,
		AgentID:    agentID,
		PlatformID: platformID,
		UserID:     userID,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
	}
}

func PermissionReplyEvent(agentID agentkit.AgentID, sessionID agentkit.SessionID, platformID, userID string, reply permission.Reply) agentkit.MessageEvent {
	return agentkit.MessageEvent{
		SessionID:  sessionID,
		AgentID:    agentID,
		PlatformID: platformID,
		UserID:     userID,
		Reply:      permission.MarshalReply(reply),
	}
}
