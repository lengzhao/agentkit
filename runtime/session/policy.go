package session

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// ActiveEntryMode selects which key active-session mapping uses.
type ActiveEntryMode string

const (
	ActiveEntryEffective ActiveEntryMode = "effective"
	ActiveEntryDelivery  ActiveEntryMode = "delivery"
)

// RoutePolicy configures how inbound routes map to conversation and workspace.
type RoutePolicy struct {
	ConversationScope SessionScope
	ActiveEntryMode   ActiveEntryMode
}

// PlatformSessionPolicy holds per-platform routing overrides applied on top of
// runner defaults.
type PlatformSessionPolicy struct {
	ActiveEntryMode ActiveEntryMode
}

// DefaultRoutePolicy is runner's default: channel conversation and workspace.
func DefaultRoutePolicy(conversationScope SessionScope) RoutePolicy {
	return RoutePolicy{
		ConversationScope: conversationScope,
		ActiveEntryMode:   ActiveEntryEffective,
	}
}

// ResolveEnvelope builds a TurnEnvelope from an inbound MessageEvent and policy.
func ResolveEnvelope(event agentkit.MessageEvent, policy RoutePolicy) agentkit.TurnEnvelope {
	env := event.Envelope
	delivery := InboundDeliveryID(event)
	platform := strings.TrimSpace(event.PlatformID)
	if platform == "" && delivery != "" {
		platform = ParseDelivery(delivery, event.UserID).Platform
	}
	if env.Route.Platform == "" && delivery != "" {
		env.Route = SessionRouteFromDelivery(platform, delivery, "")
	}
	if env.Actor.UserID == "" {
		env.Actor.UserID = strings.TrimSpace(event.UserID)
	}
	if len(env.Metadata) == 0 && len(event.Metadata) > 0 {
		env.Metadata = event.Metadata
	}
	if env.Conversation == "" {
		if delivery != "" {
			if policy.ActiveEntryMode == ActiveEntryDelivery {
				env.Conversation = string(delivery)
			} else {
				env.Conversation = string(ApplyScope(delivery, policy.ConversationScope, env.Actor.UserID))
			}
		}
	}
	if env.Workspace == "" {
		env.Workspace = workspaceFromRoute(env.Route, env.Actor.UserID)
	}
	return env
}

// ActiveEntryKey returns the stable key for /new active-session mapping.
func ActiveEntryKey(route agentkit.RouteRef, policy RoutePolicy, userID string) agentkit.SessionID {
	delivery, ok := RouteSessionID(route)
	if !ok || delivery == "" {
		return ""
	}
	switch policy.ActiveEntryMode {
	case ActiveEntryDelivery:
		return delivery
	default:
		return ApplyScope(delivery, policy.ConversationScope, userID)
	}
}

func workspaceFromRoute(route agentkit.RouteRef, userID string) string {
	delivery, ok := RouteSessionID(route)
	if !ok || delivery == "" {
		return ""
	}
	scoped := ApplyScope(delivery, ScopeChannel, userID)
	return WorkspaceKey(string(scoped))
}
