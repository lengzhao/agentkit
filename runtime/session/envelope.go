package session

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// EnvelopeFromContext returns the current turn envelope, or zero when unset.
//
// Deprecated: use rctx.EnvelopeFromContext.
func EnvelopeFromContext(ctx context.Context) agentkit.TurnEnvelope {
	return rctx.EnvelopeFromContext(ctx)
}

// ApplyEnvelopeToContext stores the turn envelope on ctx.
//
// Deprecated: use rctx.ApplyEnvelopeToContext.
func ApplyEnvelopeToContext(ctx context.Context, env agentkit.TurnEnvelope) context.Context {
	return rctx.ApplyEnvelopeToContext(ctx, env)
}

// WithRoute returns ctx with an updated envelope route.
//
// Deprecated: use rctx.WithRoute.
func WithRoute(ctx context.Context, route agentkit.RouteRef) context.Context {
	return rctx.WithRoute(ctx, route)
}

// WithConversation returns ctx with an updated envelope conversation.
//
// Deprecated: use rctx.WithConversation.
func WithConversation(ctx context.Context, conversation string) context.Context {
	return rctx.WithConversation(ctx, conversation)
}

// WithWorkspace returns ctx with an updated envelope workspace.
//
// Deprecated: use rctx.WithWorkspace.
func WithWorkspace(ctx context.Context, workspace string) context.Context {
	return rctx.WithWorkspace(ctx, workspace)
}

// WithAgentID returns ctx with an updated envelope agent id.
//
// Deprecated: use rctx.WithAgentID.
func WithAgentID(ctx context.Context, agentID agentkit.AgentID) context.Context {
	return rctx.WithAgentID(ctx, agentID)
}

// WithContextMetadata merges metadata onto the turn envelope.
//
// Deprecated: use rctx.WithContextMetadata.
func WithContextMetadata(ctx context.Context, extra map[string]any) context.Context {
	return rctx.WithContextMetadata(ctx, extra)
}

// SessionIDFromContext reports the history/lock key for the current turn.
//
// Deprecated: use rctx.SessionIDFromContext.
func SessionIDFromContext(ctx context.Context) agentkit.SessionID {
	return rctx.SessionIDFromContext(ctx)
}

// ConversationFromContext reports the history/lock key for the current turn.
//
// Deprecated: use rctx.ConversationFromContext.
func ConversationFromContext(ctx context.Context) string {
	return rctx.ConversationFromContext(ctx)
}

// PlatformFromContext reports the platform id for the current turn.
//
// Deprecated: use rctx.PlatformFromContext.
func PlatformFromContext(ctx context.Context) string {
	return rctx.PlatformFromContext(ctx)
}

// UserIDFromContext reports the end-user id for the current turn.
//
// Deprecated: use rctx.UserIDFromContext.
func UserIDFromContext(ctx context.Context) string {
	return rctx.UserIDFromContext(ctx)
}

// MetadataFromContext returns platform metadata for the current turn.
//
// Deprecated: use rctx.MetadataFromContext.
func MetadataFromContext(ctx context.Context) map[string]any {
	return rctx.MetadataFromContext(ctx)
}

// AgentIDFromContext reports the agent executing the current turn.
//
// Deprecated: use rctx.AgentIDFromContext.
func AgentIDFromContext(ctx context.Context) agentkit.AgentID {
	return rctx.AgentIDFromContext(ctx)
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
