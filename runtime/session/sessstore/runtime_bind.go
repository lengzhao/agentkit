package sessstore

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
)

// RuntimeStore returns the session runtime overlay capability when present.
func RuntimeStore(store agentkit.SessionStore) (agentkit.SessionRuntimeStore, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	rs, ok := store.(agentkit.SessionRuntimeStore)
	if !ok {
		return nil, fmt.Errorf("session store does not support runtime overrides")
	}
	return rs, nil
}

// SetSessionAgentBind sets or clears the per-session agent override.
func SetSessionAgentBind(ctx context.Context, store agentkit.SessionStore, id agentkit.SessionID, agent agentkit.AgentID) error {
	rs, err := RuntimeStore(store)
	if err != nil {
		return err
	}
	return rs.SetAgentBind(ctx, id, agent)
}

// SetSessionModelBind sets or clears the per-session model override.
func SetSessionModelBind(ctx context.Context, store agentkit.SessionStore, id agentkit.SessionID, model string) error {
	rs, err := RuntimeStore(store)
	if err != nil {
		return err
	}
	return rs.SetModelBind(ctx, id, model)
}
