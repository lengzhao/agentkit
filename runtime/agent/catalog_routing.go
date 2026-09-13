package agent

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

type catalogRoutingDeps struct {
	store        agentkit.SessionStore
	defaultAgent agentkit.AgentID
	workspace    workspace.Service
}

func resolveCatalogSessionID(ctx context.Context, store agentkit.SessionStore) (agentkit.SessionID, error) {
	sessionID := session.SessionIDFromContext(ctx)
	if sessionID == "" {
		return "", nil
	}
	if store == nil {
		return sessionID, nil
	}
	if activeStore, ok := store.(agentkit.ActiveSessionStore); ok {
		active, err := activeStore.ActiveSession(ctx, sessionID)
		if err != nil {
			return "", err
		}
		if active != "" {
			sessionID = active
		}
	}
	return sessionID, nil
}

// resolveCatalogAgentRouting matches runner inbound resolution (without per-message agent_id).
func resolveCatalogAgentRouting(ctx context.Context, deps catalogRoutingDeps) (effective, sessionBind, globalBind, loopDefault agentkit.AgentID, err error) {
	loopDefault = deps.defaultAgent
	sessionID, err := resolveCatalogSessionID(ctx, deps.store)
	if err != nil {
		return "", "", "", loopDefault, err
	}
	effective, sessionBind, globalBind, err = session.ResolveAgentID(ctx, deps.store, deps.workspace, sessionID, "")
	if err != nil {
		return "", "", "", loopDefault, err
	}
	if effective == "" && loopDefault != "" {
		effective = loopDefault
	}
	return effective, sessionBind, globalBind, loopDefault, nil
}

func configuredModel(agents []agentkit.Agent, agentID agentkit.AgentID) string {
	for _, ag := range agents {
		if ag == nil || ag.ID() != agentID {
			continue
		}
		if m, ok := ag.(interface{ ConfiguredModel() string }); ok {
			return strings.TrimSpace(m.ConfiguredModel())
		}
		return ""
	}
	return ""
}
