package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

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

// EffectiveModel returns the session model override when set, otherwise defaultModel.
func EffectiveModel(ctx context.Context, store agentkit.SessionStore, sessionID agentkit.SessionID, defaultModel string) (effective, bound string, err error) {
	effective, bound, _, err = ResolveEffectiveModel(ctx, store, nil, sessionID, "", defaultModel)
	return effective, bound, err
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
