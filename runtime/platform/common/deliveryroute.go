package common

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// DeliveryRoute is the resolved outbound/inbox target for platform tools.
type DeliveryRoute struct {
	SessionID  agentkit.SessionID
	AgentID    agentkit.AgentID
	PlatformID string
	UserID     string
}

// DeliveryRouteInput is shared by send, chat_history, and similar tools.
type DeliveryRouteInput struct {
	SessionID string
	UserID    string
}

// ResolveDeliveryRoute resolves the delivery session and platform from context.
func ResolveDeliveryRoute(ctx context.Context, input DeliveryRouteInput) (DeliveryRoute, error) {
	inbox := session.DeliveryRouteFromContext(ctx)

	var r DeliveryRoute
	switch {
	case strings.TrimSpace(input.SessionID) != "":
		r.SessionID = NormalizeDeliverySessionID(ctx, input.SessionID)
	case strings.TrimSpace(input.UserID) != "":
		r.SessionID = session.DeliveryWithUser(inbox, input.UserID)
	default:
		r.SessionID = inbox
	}
	if r.SessionID == "" {
		return DeliveryRoute{}, fmt.Errorf("requires inbox session in context, or sessionId/userId")
	}
	r.AgentID = session.AgentIDFromContext(ctx)
	r.PlatformID = session.PlatformFromContext(ctx)
	if id := strings.TrimSpace(input.UserID); id != "" {
		r.UserID = id
	} else {
		r.UserID = session.UserIDFromContext(ctx)
	}
	if p := session.ParseDelivery(r.SessionID, r.UserID).Platform; p != "" {
		if r.PlatformID == "" || strings.TrimSpace(input.SessionID) != "" {
			r.PlatformID = p
		}
	}
	return r, nil
}

// NormalizeDeliverySessionID maps slash-style targets to delivery session ids.
func NormalizeDeliverySessionID(ctx context.Context, raw string) agentkit.SessionID {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ":") {
		return agentkit.SessionID(raw)
	}
	platformID := session.PlatformFromContext(ctx)
	if platformID == "slack" && IsSlackChannelID(raw) {
		return agentkit.SessionID("slack:" + raw)
	}
	return agentkit.SessionID(raw)
}

// IsSlackChannelID reports bare Slack conversation ids (C/G/D + alnum).
func IsSlackChannelID(token string) bool {
	token = strings.TrimSpace(token)
	if len(token) < 9 {
		return false
	}
	switch token[0] {
	case 'C', 'G', 'D':
	default:
		return false
	}
	for _, r := range token[1:] {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}
