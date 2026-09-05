package session

import (
	"strings"
	"strconv"

	"github.com/lengzhao/agentkit"
)

// WorkspaceScope selects how Workspace is derived from a delivery route.
type WorkspaceScope string

const (
	WorkspaceScopeChannel WorkspaceScope = "channel"
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
	WorkspaceScope    WorkspaceScope
	ActiveEntryMode   ActiveEntryMode
}

// DefaultRoutePolicy is runner's default: channel conversation and workspace.
func DefaultRoutePolicy(conversationScope SessionScope) RoutePolicy {
	return RoutePolicy{
		ConversationScope: conversationScope,
		WorkspaceScope:    WorkspaceScopeChannel,
		ActiveEntryMode:   ActiveEntryEffective,
	}
}

// RoutePolicyForPlatform is implemented in platform_policy.go.

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
		env.Workspace = workspaceFromRoute(env.Route, policy.WorkspaceScope, env.Actor.UserID)
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

// NewConversationID returns a fresh conversation id for /new.
func NewConversationID(current string) string {
	return string(NewSessionID(agentkit.SessionID(current)))
}

// ChildConversationID returns a subagent conversation id under a parent.
func ChildConversationID(parentConversation, agentName string, seq int64) string {
	return parentConversation + ":sub:" + agentName + ":" + strconv.FormatInt(seq, 10)
}

func workspaceFromRoute(route agentkit.RouteRef, scope WorkspaceScope, userID string) string {
	delivery, ok := RouteSessionID(route)
	if !ok || delivery == "" {
		return ""
	}
	switch scope {
	case WorkspaceScopeChannel:
		scoped := ApplyScope(delivery, ScopeChannel, userID)
		return WorkspaceKey(string(scoped))
	default:
		return WorkspaceKey(string(delivery))
	}
}

func SyncMessageEvent(event agentkit.MessageEvent, env agentkit.TurnEnvelope) agentkit.MessageEvent {
	event.Envelope = env
	if event.PlatformID == "" {
		event.PlatformID = env.Route.Platform
	}
	if event.UserID == "" {
		event.UserID = env.Actor.UserID
	}
	if len(event.Metadata) == 0 && len(env.Metadata) > 0 {
		event.Metadata = env.Metadata
	}
	return event
}

func OutboundFromEnvelope(env agentkit.TurnEnvelope, typ agentkit.EventType, data []byte) agentkit.OutboundEvent {
	return agentkit.OutboundEvent{
		Route:      env.Route,
		PlatformID: env.Route.Platform,
		UserID:     env.Actor.UserID,
		Type:       typ,
		Data:       data,
	}
}
