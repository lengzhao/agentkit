package agentkit

import "context"

// EnvelopeFromContext returns the current turn envelope, or zero when unset.
func EnvelopeFromContext(ctx context.Context) TurnEnvelope {
	env, _ := ctx.Value(KeyTurnEnvelope).(TurnEnvelope)
	return env
}

// ApplyEnvelopeToContext stores the turn envelope on ctx.
func ApplyEnvelopeToContext(ctx context.Context, env TurnEnvelope) context.Context {
	return context.WithValue(ctx, KeyTurnEnvelope, env)
}

// WithRoute returns ctx with an updated envelope route.
func WithRoute(ctx context.Context, route RouteRef) context.Context {
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

// WithAgentID returns ctx with an updated envelope agent id.
func WithAgentID(ctx context.Context, agentID AgentID) context.Context {
	env := EnvelopeFromContext(ctx)
	env.AgentID = agentID
	return ApplyEnvelopeToContext(ctx, env)
}

// WithContextMetadata merges metadata onto the turn envelope.
func WithContextMetadata(ctx context.Context, extra map[string]any) context.Context {
	if len(extra) == 0 {
		return ctx
	}
	env := EnvelopeFromContext(ctx)
	meta := env.Metadata
	if meta == nil {
		meta = make(map[string]any, len(extra))
	}
	for k, v := range extra {
		meta[k] = v
	}
	env.Metadata = meta
	return ApplyEnvelopeToContext(ctx, env)
}

// SessionIDFromContext reports the history/lock key for the current turn.
func SessionIDFromContext(ctx context.Context) SessionID {
	return SessionID(ConversationFromContext(ctx))
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

// AgentIDFromContext reports the agent executing the current turn.
func AgentIDFromContext(ctx context.Context) AgentID {
	return EnvelopeFromContext(ctx).AgentID
}
