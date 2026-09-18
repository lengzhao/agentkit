package session

import (
	"context"
	"log/slog"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// ParentSessionForDelegate returns the parent session for delegation, preferring
// the turn's open session object over SessionStore.Get to avoid re-entrant locks
// (e.g. session/agent-guard while a turn is open).
func ParentSessionForDelegate(ctx context.Context, store agentkit.SessionStore, parentID agentkit.SessionID) (agentkit.Session, error) {
	if parent, ok := rctx.SessionFromContext(ctx); ok && parent.ID() == parentID {
		slog.Debug("session: resolve parent for delegate", "parent", parentID, "source", "open_turn")
		return parent, nil
	}
	slog.Debug("session: resolve parent for delegate", "parent", parentID, "source", "store_get")
	return store.Get(ctx, parentID)
}

// LoadSession opens a session by id. If id matches the turn's open session
// (KeySession), returns that object without SessionStore.Get.
func LoadSession(ctx context.Context, store agentkit.SessionStore, id agentkit.SessionID) (agentkit.Session, error) {
	if open, ok := rctx.SessionFromContext(ctx); ok && open.ID() == id {
		slog.Debug("session: load", "id", id, "source", "open_turn")
		return open, nil
	}
	slog.Debug("session: load", "id", id, "source", "store_get")
	return store.Get(ctx, id)
}
