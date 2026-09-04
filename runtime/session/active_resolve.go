package session

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// ResolveActiveSessionID maps a stable active-session entry key to the current
// history SessionID. When no /new mapping exists, entryKey is returned unchanged.
func ResolveActiveSessionID(ctx context.Context, store agentkit.SessionStore, entryKey agentkit.SessionID) (agentkit.SessionID, error) {
	if entryKey == "" {
		return "", nil
	}
	activeStore, ok := store.(agentkit.ActiveSessionStore)
	if !ok {
		return entryKey, nil
	}
	active, err := activeStore.ActiveSession(ctx, entryKey)
	if err != nil {
		return "", err
	}
	if active == "" || active == entryKey {
		return entryKey, nil
	}
	return active, nil
}
