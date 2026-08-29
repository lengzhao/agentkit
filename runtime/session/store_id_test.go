package session_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestAgentStoreSessionIDChatAPIUsesDelivery(t *testing.T) {
	t.Parallel()

	effective := agentkit.SessionID("chat-api:default_channel")
	delivery := agentkit.SessionID("chat-api:default_channel:t:conv_abc")
	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeySessionID, effective)
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, delivery)
	ctx = context.WithValue(ctx, agentkit.KeyPlatformID, "chat-api")

	if got := session.AgentStoreSessionID(ctx); got != delivery {
		t.Fatalf("got %q want delivery %q", got, delivery)
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
