package sessbind

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// ResolveAgentID applies the shared level stack (session, user, thread, channel,
// global), then an explicit event agent id. The bind results are the stored
// values at each legacy level (session/channel/global) so existing callers and
// renderers keep working; thread/user are surfaced only through BindStack.
func ResolveAgentID(
	ctx context.Context,
	store agentkit.SessionStore,
	ws workspace.Service,
	sessionID agentkit.SessionID,
	entryKey agentkit.SessionID,
	eventAgent agentkit.AgentID,
) (effective, sessionBind, channelBind, globalBind agentkit.AgentID, err error) {
	stack := StackFromContext(ctx, ws).WithSession(sessionID).WithChannel(entryKey)
	effectiveVal, hits, err := stack.Effective(ctx, store, AgentBinding{})
	if err != nil {
		return "", "", "", "", err
	}
	sessionBind = agentkit.AgentID(hits[LevelSession])
	channelBind = agentkit.AgentID(hits[LevelChannel])
	globalBind = agentkit.AgentID(hits[LevelGlobal])
	if effectiveVal != "" {
		return agentkit.AgentID(effectiveVal), sessionBind, channelBind, globalBind, nil
	}
	if id := strings.TrimSpace(string(eventAgent)); id != "" {
		return agentkit.AgentID(id), sessionBind, channelBind, globalBind, nil
	}
	return "", sessionBind, channelBind, globalBind, nil
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

// ChannelAgentBind returns the channel default for entryKey (stable delivery /
// active-entry id). Falls back to legacy local:runtime.json when tenant-scoped.
func ChannelAgentBind(ctx context.Context, store agentkit.SessionStore, ws workspace.Service, entryKey agentkit.SessionID) (agentkit.AgentID, error) {
	if entryKey != "" && store != nil {
		id, err := AgentBind(ctx, store, channelAgentBindKey(entryKey))
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(string(id)) != "" {
			return id, nil
		}
	}
	if ws == nil {
		return "", nil
	}
	data, err := loadChannelRuntimeLegacy(ctx, ws)
	if err != nil {
		return "", err
	}
	return data.AgentID, nil
}

// SetChannelAgentBind sets or clears the channel default for entryKey.
func SetChannelAgentBind(ctx context.Context, store agentkit.SessionStore, ws workspace.Service, entryKey agentkit.SessionID, agent agentkit.AgentID) error {
	agent = agentkit.AgentID(strings.TrimSpace(string(agent)))
	if entryKey == "" {
		return errors.New("channel entry key is required")
	}
	if store == nil {
		return errors.New("session store is required for channel agent binding")
	}
	if err := SetSessionAgentBind(ctx, store, channelAgentBindKey(entryKey), agent); err != nil {
		return err
	}
	return clearChannelRuntimeLegacy(ctx, ws)
}

// channelBindKey reserves a separate runtime key for a channel-scoped override
// of the given kind. The initial session can have the same ID as entryKey, so
// storing the channel override directly under entryKey would overwrite that
// session's override.
func channelBindKey(kind string, entryKey agentkit.SessionID) agentkit.SessionID {
	return agentkit.SessionID("channel-" + kind + ":" + string(entryKey))
}

// channelAgentBindKey reserves the channel key for the agent kind (kept for
// callers/tests that referenced it before kinds were parameterized).
func channelAgentBindKey(entryKey agentkit.SessionID) agentkit.SessionID {
	return channelBindKey("agent", entryKey)
}

// ChannelModelBind returns the channel default model for entryKey, or "" when
// unset. Mirrors ChannelAgentBind so agent and model share the same level stack.
func ChannelModelBind(ctx context.Context, store agentkit.SessionStore, entryKey agentkit.SessionID) (string, error) {
	if entryKey == "" || store == nil {
		return "", nil
	}
	return ModelBind(ctx, store, channelBindKey("model", entryKey))
}

// SetChannelModelBind sets or clears the channel default model for entryKey.
func SetChannelModelBind(ctx context.Context, store agentkit.SessionStore, entryKey agentkit.SessionID, model string) error {
	if entryKey == "" {
		return errors.New("channel entry key is required")
	}
	if store == nil {
		return errors.New("session store is required for channel model binding")
	}
	return SetSessionModelBind(ctx, store, channelBindKey("model", entryKey), model)
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

// ResolveEffectiveModel applies the shared level stack (session, user, thread,
// channel, global-per-agent), then defaultModel. Agent and model therefore use
// the identical level order and key computation; only the value/storage differ.
func ResolveEffectiveModel(
	ctx context.Context,
	store agentkit.SessionStore,
	ws workspace.Service,
	sessionID agentkit.SessionID,
	agentID agentkit.AgentID,
	defaultModel string,
) (effective, sessionOverride, globalOverride string, err error) {
	effective, _, hits, err := ResolveModelBinding(ctx, store, ws, sessionID, agentID, defaultModel)
	if err != nil {
		return "", "", "", err
	}
	return effective, hits[LevelSession], hits[LevelGlobal], nil
}

// ResolveModelBinding is ResolveEffectiveModel plus the winning level and the
// full hit map, so the /model command can report which scope won rather than
// inferring it from a non-empty finer hit.
func ResolveModelBinding(
	ctx context.Context,
	store agentkit.SessionStore,
	ws workspace.Service,
	sessionID agentkit.SessionID,
	agentID agentkit.AgentID,
	defaultModel string,
) (effective string, winner Level, hits map[Level]string, err error) {
	stack := StackFromContext(ctx, ws).WithSession(sessionID)
	effective, winner, hits, err = stack.EffectiveWithLevel(ctx, store, ModelBinding{AgentID: agentID})
	if err != nil {
		return "", LevelNone, nil, err
	}
	if effective != "" {
		return effective, winner, hits, nil
	}
	return strings.TrimSpace(defaultModel), LevelNone, hits, nil
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

// GlobalModelBinds returns every entry in global:runtime.json models, or nil
// when unset. Commands use it to list configured overrides.
func GlobalModelBinds(ctx context.Context, ws workspace.Service) (map[string]string, error) {
	if ws == nil {
		return nil, nil
	}
	data, err := loadGlobalRuntime(ctx, ws)
	if err != nil {
		return nil, err
	}
	if len(data.Models) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(data.Models))
	for k, v := range data.Models {
		out[k] = v
	}
	return out, nil
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
