package session

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
)

// EnvelopeFromContext returns the current turn envelope, or zero when unset.
func EnvelopeFromContext(ctx context.Context) agentkit.TurnEnvelope {
	return agentkit.EnvelopeFromContext(ctx)
}

// ApplyEnvelopeToContext stores the turn envelope on ctx.
func ApplyEnvelopeToContext(ctx context.Context, env agentkit.TurnEnvelope) context.Context {
	return agentkit.ApplyEnvelopeToContext(ctx, env)
}

// WithRoute returns ctx with an updated envelope route.
func WithRoute(ctx context.Context, route agentkit.RouteRef) context.Context {
	return agentkit.WithRoute(ctx, route)
}

// WithConversation returns ctx with an updated envelope conversation.
func WithConversation(ctx context.Context, conversation string) context.Context {
	return agentkit.WithConversation(ctx, conversation)
}

// WithWorkspace returns ctx with an updated envelope workspace.
func WithWorkspace(ctx context.Context, workspace string) context.Context {
	return agentkit.WithWorkspace(ctx, workspace)
}

// WithAgentID returns ctx with an updated envelope agent id.
func WithAgentID(ctx context.Context, agentID agentkit.AgentID) context.Context {
	return agentkit.WithAgentID(ctx, agentID)
}

// SessionIDFromContext reports the history/lock key for the current turn.
func SessionIDFromContext(ctx context.Context) agentkit.SessionID {
	return agentkit.SessionIDFromContext(ctx)
}

// ConversationFromContext reports the history/lock key for the current turn.
func ConversationFromContext(ctx context.Context) string {
	return agentkit.ConversationFromContext(ctx)
}

// PlatformFromContext reports the platform id for the current turn.
func PlatformFromContext(ctx context.Context) string {
	return agentkit.PlatformFromContext(ctx)
}

// UserIDFromContext reports the end-user id for the current turn.
func UserIDFromContext(ctx context.Context) string {
	return agentkit.UserIDFromContext(ctx)
}

// MetadataFromContext returns platform metadata for the current turn.
func MetadataFromContext(ctx context.Context) map[string]any {
	return agentkit.MetadataFromContext(ctx)
}

// AgentIDFromContext reports the agent executing the current turn.
func AgentIDFromContext(ctx context.Context) agentkit.AgentID {
	return agentkit.AgentIDFromContext(ctx)
}

// ActiveEntryKeyFromContext derives the stable /new active-session mapping key
// from the turn route. Conversation may already be resolved to a child session.
func ActiveEntryKeyFromContext(ctx context.Context) agentkit.SessionID {
	env := agentkit.EnvelopeFromContext(ctx)
	platform := agentkit.PlatformFromContext(ctx)
	if platform == "" {
		platform = strings.TrimSpace(env.Route.Platform)
	}
	policy := RoutePolicyForPlatform(platform, DefaultRoutePolicy(SessionScopeFromContext(ctx)))
	return ActiveEntryKey(env.Route, policy, agentkit.UserIDFromContext(ctx))
}

// WorkspaceFromContext reports the tenant workspace key for the current turn.
// Runner should set TurnEnvelope.Workspace explicitly; fallback derivation logs a warning.
func WorkspaceFromContext(ctx context.Context) string {
	if env := agentkit.EnvelopeFromContext(ctx); env.Workspace != "" {
		return env.Workspace
	}
	if delivery := DeliveryRouteFromContext(ctx); delivery != "" {
		slog.Warn("workspace derived from delivery route; set TurnEnvelope.Workspace at ingress",
			"delivery", delivery)
		scoped := ApplyScope(delivery, ScopeChannel, agentkit.UserIDFromContext(ctx))
		return WorkspaceKey(string(scoped))
	}
	if conv := agentkit.ConversationFromContext(ctx); conv != "" {
		slog.Warn("workspace derived from conversation; set TurnEnvelope.Workspace at ingress",
			"conversation", conv)
		return WorkspaceKey(conv)
	}
	return ""
}
