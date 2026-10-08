package subagent

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
)

// telemetryModel returns the model id for Loop-backed delegations (definition, then agent config).
func telemetryModel(def capsubagent.Definition, ag agentkit.Agent) string {
	if m := strings.TrimSpace(def.Model); m != "" {
		return m
	}
	if ag == nil {
		return ""
	}
	if cm, ok := ag.(interface{ ConfiguredModel() string }); ok {
		return strings.TrimSpace(cm.ConfiguredModel())
	}
	return ""
}

// inprocessTelemetryModel is the model id recorded for Langfuse and the child LLM:
// agents/<name>.md model: is the default, then session/global binds for the child session.
func inprocessTelemetryModel(
	ctx context.Context,
	store agentkit.SessionStore,
	ws workspace.Service,
	childID agentkit.SessionID,
	agentID agentkit.AgentID,
	def capsubagent.Definition,
) string {
	defaultModel := strings.TrimSpace(def.Model)
	if store == nil {
		return defaultModel
	}
	effective, _, _, err := sessbind.ResolveEffectiveModel(ctx, store, ws, childID, agentID, defaultModel)
	if err != nil {
		return defaultModel
	}
	if m := strings.TrimSpace(effective); m != "" {
		return m
	}
	return defaultModel
}
