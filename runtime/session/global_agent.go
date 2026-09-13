package session

import (
	"context"
	"errors"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// ResolveAgentID applies session bind, global default, then explicit event agent id.
func ResolveAgentID(
	ctx context.Context,
	store agentkit.SessionStore,
	ws workspace.Service,
	sessionID agentkit.SessionID,
	eventAgent agentkit.AgentID,
) (effective, sessionBind, globalBind agentkit.AgentID, err error) {
	sessionBind, err = AgentBind(ctx, store, sessionID)
	if err != nil {
		return "", "", "", err
	}
	if id := strings.TrimSpace(string(sessionBind)); id != "" {
		return agentkit.AgentID(id), sessionBind, "", nil
	}
	if ws != nil {
		globalBind, err = GlobalAgentBind(ctx, ws)
		if err != nil {
			return "", "", "", err
		}
		if id := strings.TrimSpace(string(globalBind)); id != "" {
			return agentkit.AgentID(id), "", globalBind, nil
		}
	}
	if id := strings.TrimSpace(string(eventAgent)); id != "" {
		return agentkit.AgentID(id), "", "", nil
	}
	return "", "", "", nil
}

// AgentBind returns the per-session agent override, or "" when unset.
func AgentBind(ctx context.Context, store agentkit.SessionStore, id agentkit.SessionID) (agentkit.AgentID, error) {
	if store == nil || id == "" {
		return "", nil
	}
	bindStore, ok := store.(agentkit.AgentBindStore)
	if !ok {
		return "", nil
	}
	return bindStore.AgentBind(ctx, id)
}

// GlobalAgentBind returns the workspace-wide default agent id, or "" when unset.
func GlobalAgentBind(ctx context.Context, ws workspace.Service) (agentkit.AgentID, error) {
	if ws == nil {
		return "", nil
	}
	data, err := loadGlobalRuntime(ctx, ws)
	if err != nil {
		return "", err
	}
	return data.AgentID, nil
}

// SetGlobalAgentBind sets or clears the workspace-wide default agent id.
func SetGlobalAgentBind(ctx context.Context, ws workspace.Service, agent agentkit.AgentID) error {
	if ws == nil {
		return errors.New("workspace is required for global agent binding")
	}
	data, err := loadGlobalRuntime(ctx, ws)
	if err != nil {
		return err
	}
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(agent)))
	return saveGlobalRuntime(ctx, ws, data)
}
