package delivery

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// Sender delivers outbound events to a transport. agentkit.Platform implements this.
type Sender interface {
	Send(context.Context, agentkit.OutboundEvent) error
}

// RouteInput is shared by send, chat_history, and similar tools.
type RouteInput struct {
	SessionID string
	UserID    string
}

// AssistantMessageOptions configures proactive assistant outbound delivery.
type AssistantMessageOptions struct {
	Route RouteInput
	// Raw skips platform markdown conversion when the transport supports it.
	Raw bool
	// UseContextEmit delivers through the per-turn OutboundEmit hook when the route
	// is the current inbox (empty SessionID and UserID).
	UseContextEmit bool
}

// Assistant resolves delivery routes and sends proactive assistant messages.
type Assistant interface {
	SendAssistantMessage(context.Context, Sender, []agentkit.ContentPart, AssistantMessageOptions) error
	SendProactiveInboxText(context.Context, Sender, string) error
}
