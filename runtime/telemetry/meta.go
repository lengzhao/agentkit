package telemetry

import (
	"context"

	"github.com/lengzhao/agentkit"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/session"
)

// ObservationMetaFromContext fills agent and session ids from ctx when unset.
func ObservationMetaFromContext(ctx context.Context, meta captelemetry.ObservationMeta) captelemetry.ObservationMeta {
	if meta.AgentID == "" {
		if id := string(session.AgentIDFromContext(ctx)); id != "" {
			meta.AgentID = id
		} else if id := agentIDFromEnvelope(ctx); id != "" {
			meta.AgentID = id
		}
	}
	if meta.SessionID == "" {
		if id := string(session.SessionIDFromContext(ctx)); id != "" {
			meta.SessionID = id
		} else if id := conversationFromEnvelope(ctx); id != "" {
			meta.SessionID = id
		}
	}
	return meta
}

// ContextObservationAttrs returns turn-scoped metadata for Langfuse observations.
func ContextObservationAttrs(ctx context.Context) map[string]string {
	out := map[string]string{}
	if id := TurnIDFrom(ctx); id != "" {
		out["turn_id"] = id
	}
	env := session.EnvelopeFromContext(ctx)
	if env.Route.Platform != "" {
		out["platform_id"] = env.Route.Platform
	}
	if env.Actor.UserID != "" {
		out["user_id"] = env.Actor.UserID
	}
	if env.Workspace != "" {
		out["workspace_key"] = env.Workspace
	}
	if env.Conversation != "" {
		out["conversation_id"] = env.Conversation
	}
	return out
}

// EnrichEventAttrs adds agent_id and session_id from ctx when missing.
func EnrichEventAttrs(ctx context.Context, attrs map[string]string) map[string]string {
	out := make(map[string]string, len(attrs)+2)
	for k, v := range attrs {
		out[k] = v
	}
	if _, ok := out["agent_id"]; !ok {
		if id := agentIDFromEnvelope(ctx); id != "" {
			out["agent_id"] = id
		}
	}
	if _, ok := out["session_id"]; !ok {
		if id := conversationFromEnvelope(ctx); id != "" {
			out["session_id"] = id
		}
	}
	return out
}

func agentIDFromEnvelope(ctx context.Context) string {
	if env, ok := ctx.Value(agentkit.KeyTurnEnvelope).(agentkit.TurnEnvelope); ok && env.AgentID != "" {
		return string(env.AgentID)
	}
	return ""
}

func conversationFromEnvelope(ctx context.Context) string {
	if env, ok := ctx.Value(agentkit.KeyTurnEnvelope).(agentkit.TurnEnvelope); ok && env.Conversation != "" {
		return env.Conversation
	}
	return ""
}
