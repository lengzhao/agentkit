package sessbind

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// SubagentModelWildcardKey is the global models key that applies to every
// in-process subagent without a more specific entry. Written by
// `/model -g sub * <model>`; it never affects the main agent.
const SubagentModelWildcardKey agentkit.AgentID = "sub:*"

// ResolveSubagentModel resolves the model for an in-process subagent step.
//
// Priority (definition author > explicit subagent config > general default > fallback):
//  1. defModel — agents/<name>.md `model:` is a capability contract authored
//     together with the prompt and modalities; runtime overrides never win.
//  2. global models[<childAgentID>] — the explicit per-subagent config written
//     by `/model -g sub <name> <model>`.
//  3. global models[SubagentModelWildcardKey] — the all-subagents policy
//     written by `/model -g sub * <model>`; main agent is unaffected.
//  4. global models[<parentAgentID>] — the parent agent's `/model -g` choice,
//     the general default specific keys refine.
//
// defModel is also the final fallback when nothing else is set. Neither the
// child session's bind nor the parent conversation's session bind participate:
// per-delegation ids have no /model entry point, and the user's in-conversation
// /model targets the main agent only — subagent models are explicit config,
// not implicit inheritance.
func ResolveSubagentModel(
	ctx context.Context,
	ws workspace.Service,
	parentAgentID agentkit.AgentID,
	childAgentID agentkit.AgentID,
	defModel string,
) string {
	defModel = strings.TrimSpace(defModel)
	if defModel != "" {
		return defModel
	}
	if ws == nil {
		return defModel
	}
	if id := strings.TrimSpace(string(childAgentID)); id != "" {
		if m, err := GlobalModelBind(ctx, ws, agentkit.AgentID(id)); err == nil {
			if m = strings.TrimSpace(m); m != "" {
				return m
			}
		}
	}
	if m, err := GlobalModelBind(ctx, ws, SubagentModelWildcardKey); err == nil {
		if m = strings.TrimSpace(m); m != "" {
			return m
		}
	}
	if id := strings.TrimSpace(string(parentAgentID)); id != "" {
		if m, err := GlobalModelBind(ctx, ws, agentkit.AgentID(id)); err == nil {
			if m = strings.TrimSpace(m); m != "" {
				return m
			}
		}
	}
	return defModel
}
