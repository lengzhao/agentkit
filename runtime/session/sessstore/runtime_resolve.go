package sessstore

import (
	"context"
	"errors"
	"fmt"
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
	rs, ok := store.(agentkit.SessionRuntimeStore)
	if !ok {
		return "", nil
	}
	return rs.AgentBind(ctx, id)
}

// ModelBind returns the per-session model override, or "" when unset.
func ModelBind(ctx context.Context, store agentkit.SessionStore, id agentkit.SessionID) (string, error) {
	if store == nil || id == "" {
		return "", nil
	}
	rs, ok := store.(agentkit.SessionRuntimeStore)
	if !ok {
		return "", nil
	}
	return rs.ModelBind(ctx, id)
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

// ResolveEffectiveModel applies session override, then global (per agent), then defaultModel.
func ResolveEffectiveModel(
	ctx context.Context,
	store agentkit.SessionStore,
	ws workspace.Service,
	sessionID agentkit.SessionID,
	agentID agentkit.AgentID,
	defaultModel string,
) (effective, sessionOverride, globalOverride string, err error) {
	sessionOverride, err = ModelBind(ctx, store, sessionID)
	if err != nil {
		return "", "", "", err
	}
	if m := strings.TrimSpace(sessionOverride); m != "" {
		return m, sessionOverride, "", nil
	}
	if ws != nil && agentID != "" {
		globalOverride, err = GlobalModelBind(ctx, ws, agentID)
		if err != nil {
			return "", "", "", err
		}
		if m := strings.TrimSpace(globalOverride); m != "" {
			return m, "", globalOverride, nil
		}
	}
	def := strings.TrimSpace(defaultModel)
	return def, "", "", nil
}

// GlobalModelBind returns the global default model for agentID, or "" when unset.
func GlobalModelBind(ctx context.Context, ws workspace.Service, agentID agentkit.AgentID) (string, error) {
	if ws == nil {
		return "", nil
	}
	data, err := loadGlobalRuntime(ctx, ws)
	if err != nil {
		return "", err
	}
	if data.Models == nil {
		return "", nil
	}
	return strings.TrimSpace(data.Models[string(agentID)]), nil
}

// SetGlobalModelBind sets or clears the global default model for agentID.
func SetGlobalModelBind(ctx context.Context, ws workspace.Service, agentID agentkit.AgentID, model string) error {
	if ws == nil {
		return fmt.Errorf("workspace is required for global model binding")
	}
	agentKey := strings.TrimSpace(string(agentID))
	if agentKey == "" {
		return fmt.Errorf("agent id is required")
	}
	model = strings.TrimSpace(model)
	data, err := loadGlobalRuntime(ctx, ws)
	if err != nil {
		return err
	}
	if data.Models == nil {
		data.Models = make(map[string]string)
	}
	if model == "" {
		delete(data.Models, agentKey)
	} else {
		data.Models[agentKey] = model
	}
	return saveGlobalRuntime(ctx, ws, data)
}
