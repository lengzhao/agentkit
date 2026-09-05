package telemetry

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// ObservationMetaFromContext fills agent and session ids from ctx when unset.
func ObservationMetaFromContext(ctx context.Context, meta ObservationMeta) ObservationMeta {
	if meta.AgentID == "" {
		if id := agentIDFromEnvelope(ctx); id != "" {
			meta.AgentID = id
		}
	}
	if meta.SessionID == "" {
		if id := conversationFromEnvelope(ctx); id != "" {
			meta.SessionID = id
		}
	}
	return meta
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
