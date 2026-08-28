package telemetry

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// ObservationMetaFromContext fills agent and session ids from ctx when unset.
func ObservationMetaFromContext(ctx context.Context, meta ObservationMeta) ObservationMeta {
	if meta.AgentID == "" {
		if id, ok := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID); ok && id != "" {
			meta.AgentID = string(id)
		}
	}
	if meta.SessionID == "" {
		if id, ok := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID); ok && id != "" {
			meta.SessionID = string(id)
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
		if id, ok := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID); ok && id != "" {
			out["agent_id"] = string(id)
		}
	}
	if _, ok := out["session_id"]; !ok {
		if id, ok := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID); ok && id != "" {
			out["session_id"] = string(id)
		}
	}
	return out
}
