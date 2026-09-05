package agenttest

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

// TempFileStore creates a session/store rooted at t.TempDir().
func TempFileStore(t *testing.T) (agentkit.SessionStore, string) {
	t.Helper()
	root := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{
		Workspace: rtworkspace.Static(root),
	})
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	return store, root
}

// SessionEvents reads all events from a session id via the store.
func SessionEvents(t *testing.T, ctx context.Context, store agentkit.SessionStore, id agentkit.SessionID) []agentkit.SessionEvent {
	t.Helper()
	sess, err := store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	return events
}
