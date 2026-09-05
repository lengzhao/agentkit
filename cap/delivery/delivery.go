package delivery

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// Sender delivers outbound events to a transport. agentkit.Platform implements this.
type Sender interface {
	Send(context.Context, agentkit.OutboundEvent) error
}

// Route is the resolved outbound/inbox target for delivery tools.
type Route struct {
	SessionID  agentkit.SessionID
	AgentID    agentkit.AgentID
	PlatformID string
	UserID     string
}

// RouteInput is shared by send, chat_history, and similar tools.
type RouteInput struct {
	SessionID string
	UserID    string
}
