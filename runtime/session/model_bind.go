package session

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// ModelBind returns the per-session model override, or "" when unset.
func ModelBind(ctx context.Context, store agentkit.SessionStore, id agentkit.SessionID) (string, error) {
	bindStore, ok := store.(agentkit.ModelBindStore)
	if !ok {
		return "", nil
	}
	return bindStore.ModelBind(ctx, id)
}
