package session

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
)

// EnvelopeFromContext returns the current turn envelope, or zero when unset.
func EnvelopeFromContext(ctx context.Context) agentkit.TurnEnvelope {
	env, _ := ctx.Value(agentkit.KeyTurnEnvelope).(agentkit.TurnEnvelope)
	return env
}

// ApplyEnvelopeToContext stores the turn envelope on ctx.
func ApplyEnvelopeToContext(ctx context.Context, env agentkit.TurnEnvelope) context.Context {
	return context.WithValue(ctx, agentkit.KeyTurnEnvelope, env)
}

// WithRoute returns ctx with an updated envelope route.
func WithRoute(ctx context.Context, route agentkit.RouteRef) context.Context {
	env := EnvelopeFromContext(ctx)
	env.Route = route
	return ApplyEnvelopeToContext(ctx, env)
}

// WithConversation returns ctx with an updated envelope conversation.
func WithConversation(ctx context.Context, conversation string) context.Context {
	env := EnvelopeFromContext(ctx)
	env.Conversation = conversation
	return ApplyEnvelopeToContext(ctx, env)
}

// WithWorkspace returns ctx with an updated envelope workspace.
func WithWorkspace(ctx context.Context, workspace string) context.Context {
	env := EnvelopeFromContext(ctx)
	env.Workspace = workspace
	return ApplyEnvelopeToContext(ctx, env)
}

// ActiveEntryKeyFromContext derives the stable /new active-session mapping key
// from the turn route. Conversation may already be resolved to a child session.
func ActiveEntryKeyFromContext(ctx context.Context) agentkit.SessionID {
	env := EnvelopeFromContext(ctx)
	platform := PlatformFromContext(ctx)
	if platform == "" {
		platform = strings.TrimSpace(env.Route.Platform)
	}
	policy := RoutePolicyForPlatform(platform, DefaultRoutePolicy(ScopeChannel))
	return ActiveEntryKey(env.Route, policy, UserIDFromContext(ctx))
}

// SessionIDFromContext reports the history/lock key for the current turn.
func SessionIDFromContext(ctx context.Context) agentkit.SessionID {
	return agentkit.SessionID(ConversationFromContext(ctx))
}

// ConversationFromContext reports the history/lock key for the current turn.
func ConversationFromContext(ctx context.Context) string {
	if env := EnvelopeFromContext(ctx); env.Conversation != "" {
		return env.Conversation
	}
	return ""
}

// PlatformFromContext reports the platform id for the current turn.
func PlatformFromContext(ctx context.Context) string {
	if env := EnvelopeFromContext(ctx); env.Route.Platform != "" {
		return env.Route.Platform
	}
	return ""
}

// UserIDFromContext reports the end-user id for the current turn.
func UserIDFromContext(ctx context.Context) string {
	if env := EnvelopeFromContext(ctx); env.Actor.UserID != "" {
		return env.Actor.UserID
	}
	return ""
}

// MetadataFromContext returns platform metadata for the current turn.
func MetadataFromContext(ctx context.Context) map[string]any {
	if env := EnvelopeFromContext(ctx); len(env.Metadata) > 0 {
		return env.Metadata
	}
	return nil
}

// WithAgentID returns ctx with an updated envelope agent id.
func WithAgentID(ctx context.Context, agentID agentkit.AgentID) context.Context {
	env := EnvelopeFromContext(ctx)
	env.AgentID = agentID
	return ApplyEnvelopeToContext(ctx, env)
}

// AgentIDFromContext reports the agent executing the current turn.
func AgentIDFromContext(ctx context.Context) agentkit.AgentID {
	return EnvelopeFromContext(ctx).AgentID
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
		scoped := ApplyScope(delivery, ScopeChannel, UserIDFromContext(ctx))
		return WorkspaceKey(string(scoped))
	}
	if conv := ConversationFromContext(ctx); conv != "" {
		slog.Warn("workspace derived from conversation; set TurnEnvelope.Workspace at ingress",
			"conversation", conv)
		return WorkspaceKey(conv)
	}
	return ""
}
