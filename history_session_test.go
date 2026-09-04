package agentkit_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestResolveHistorySessionIDUsesStoreSession(t *testing.T) {
	t.Parallel()

	effective := agentkit.SessionID("chat-api:default_channel")
	logical := agentkit.SessionID("chat-api:default_channel:t:conv_abc:new:20260829")
	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeySessionID, effective)
	ctx = context.WithValue(ctx, agentkit.KeyStoreSessionID, logical)

	if got := agentkit.ResolveHistorySessionID(ctx); got != logical {
		t.Fatalf("got %q want logical %q", got, logical)
	}
}

func TestResolveHistorySessionIDIgnoresParentStoreInSubagent(t *testing.T) {
	t.Parallel()

	parentStore := agentkit.SessionID("chat-api:nex-channel")
	child := agentkit.SessionID("sub:chat-api:nex-channel:meetingbot:4")
	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeyInSubagent, true)
	ctx = context.WithValue(ctx, agentkit.KeySessionID, child)
	ctx = context.WithValue(ctx, agentkit.KeyStoreSessionID, parentStore)

	if got := agentkit.ResolveHistorySessionID(ctx); got != child {
		t.Fatalf("got %q want child %q", got, child)
	}
}

func TestHistorySessionIDPrefersPreResolvedKey(t *testing.T) {
	t.Parallel()

	effective := agentkit.SessionID("chat-api:default_channel")
	store := agentkit.SessionID("chat-api:default_channel:t:conv_abc")
	resolved := agentkit.SessionID("chat-api:default_channel:t:conv_xyz")
	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeySessionID, effective)
	ctx = context.WithValue(ctx, agentkit.KeyStoreSessionID, store)
	ctx = context.WithValue(ctx, agentkit.KeyHistorySessionID, resolved)

	if got := agentkit.HistorySessionID(ctx); got != resolved {
		t.Fatalf("got %q want pre-resolved %q", got, resolved)
	}
}
