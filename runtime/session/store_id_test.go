package session_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestAgentStoreSessionIDUsesLogicalStoreSession(t *testing.T) {
	t.Parallel()

	effective := agentkit.SessionID("chat-api:default_channel")
	logical := agentkit.SessionID("chat-api:default_channel:t:conv_abc:new:20260829")
	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeySessionID, effective)
	ctx = context.WithValue(ctx, agentkit.KeyStoreSessionID, logical)

	if got := session.AgentStoreSessionID(ctx); got != logical {
		t.Fatalf("got %q want logical %q", got, logical)
	}
}

func TestAgentStoreSessionIDIgnoresParentStoreInSubagent(t *testing.T) {
	t.Parallel()

	parentStore := agentkit.SessionID("chat-api:nex-channel")
	child := agentkit.SessionID("sub:chat-api:nex-channel:meetingbot:4")
	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeyInSubagent, true)
	ctx = context.WithValue(ctx, agentkit.KeySessionID, child)
	ctx = context.WithValue(ctx, agentkit.KeyStoreSessionID, parentStore)

	if got := session.AgentStoreSessionID(ctx); got != child {
		t.Fatalf("got %q want child %q", got, child)
	}
}

func TestAgentStoreSessionIDSlackKeepsEffective(t *testing.T) {
	t.Parallel()

	effective := agentkit.SessionID("slack:C001")
	delivery := agentkit.SessionID("slack:C001:t:123.456")
	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeySessionID, effective)
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, delivery)
	ctx = context.WithValue(ctx, agentkit.KeyPlatformID, "slack")

	if got := session.AgentStoreSessionID(ctx); got != effective {
		t.Fatalf("got %q want effective %q", got, effective)
	}
}
