package rctx

import (
	"context"
	"log/slog"
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
	ConversationScope agentkit.SessionScope
	ActiveEntryMode   ActiveEntryMode
}

// PlatformSessionPolicy holds per-platform routing overrides applied on top of
// runner defaults.
type PlatformSessionPolicy struct {
	ActiveEntryMode ActiveEntryMode
}

// DefaultRoutePolicy is runner's default: channel conversation and workspace.
func DefaultRoutePolicy(conversationScope agentkit.SessionScope) RoutePolicy {
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
	scoped := ApplyScope(delivery, agentkit.SessionScopeChannel, userID)
	return WorkspaceKey(string(scoped))
}

var platformSessionPolicies = map[string]PlatformSessionPolicy{
	"chat-api": {ActiveEntryMode: ActiveEntryDelivery},
}

// RegisterPlatformPolicy registers or overrides routing policy for a platform id.
func RegisterPlatformPolicy(platform string, policy PlatformSessionPolicy) {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return
	}
	platformSessionPolicies[platform] = policy
}

// PlatformSessionPolicyFor returns overrides for a platform, or zero when unset.
func PlatformSessionPolicyFor(platform string) PlatformSessionPolicy {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return PlatformSessionPolicy{}
	}
	return platformSessionPolicies[platform]
}

// RoutePolicyForPlatform merges runner defaults with platform-specific overrides.
func RoutePolicyForPlatform(platform string, base RoutePolicy) RoutePolicy {
	if p := PlatformSessionPolicyFor(platform); p.ActiveEntryMode != "" {
		base.ActiveEntryMode = p.ActiveEntryMode
	}
	return base
}

// ActiveSessionEntryKey returns the stable session-store key used for /new
// active-session mapping.
func ActiveSessionEntryKey(platform string, delivery agentkit.SessionID, scope agentkit.SessionScope, userID string) agentkit.SessionID {
	if delivery == "" {
		return ""
	}
	policy := RoutePolicyForPlatform(platform, DefaultRoutePolicy(scope))
	return ActiveEntryKey(SessionRouteFromDelivery(platform, delivery, ""), policy, userID)
}

// ActiveEntryKeyFromContext derives the stable /new active-session mapping key
// from the turn route. Conversation may already be resolved to a child session.
func ActiveEntryKeyFromContext(ctx context.Context) agentkit.SessionID {
	env := EnvelopeFromContext(ctx)
	platform := PlatformFromContext(ctx)
	if platform == "" {
		platform = strings.TrimSpace(env.Route.Platform)
	}
	policy := RoutePolicyForPlatform(platform, DefaultRoutePolicy(SessionScopeFromContext(ctx)))
	return ActiveEntryKey(env.Route, policy, UserIDFromContext(ctx))
}

// WorkspaceFromContext reports the tenant workspace key for the current turn.
// Runner should set TurnEnvelope.Workspace explicitly; fallback derivation logs a warning.
func WorkspaceFromContext(ctx context.Context) string {
	if env := EnvelopeFromContext(ctx); env.Workspace != "" {
		return env.Workspace
	}
	if delivery := DeliveryRouteFromContext(ctx); delivery != "" {
		slog.Warn("workspace derived from delivery route; set TurnEnvelope.Workspace at ingress",
			"delivery", delivery)
		scoped := ApplyScope(delivery, agentkit.SessionScopeChannel, UserIDFromContext(ctx))
		return WorkspaceKey(string(scoped))
	}
	if conv := ConversationFromContext(ctx); conv != "" {
		slog.Warn("workspace derived from conversation; set TurnEnvelope.Workspace at ingress",
			"conversation", conv)
		return WorkspaceKey(conv)
	}
	return ""
}
