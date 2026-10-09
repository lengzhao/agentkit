package subagent

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
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

// inprocessTelemetryModel is the model id recorded for Langfuse and the child LLM.
// It resolves exactly like the child runtime does (sessbind.ResolveSubagentModel):
// agents/<name>.md `model:` first, then global binds keyed by the child agent
// (explicit /model -g sub <name> config), the subagents wildcard (sub:*), then
// by the parent agent.
func inprocessTelemetryModel(
	ctx context.Context,
	ws workspace.Service,
	agentID agentkit.AgentID,
	def capsubagent.Definition,
) string {
	scope, ok := rctx.SubagentModelScopeFrom(ctx)
	if !ok {
		return strings.TrimSpace(def.Model)
	}
	return sessbind.ResolveSubagentModel(ctx, ws, scope.ParentAgentID, agentID, def.Model)
}
