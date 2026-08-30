package session_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestChannelKeyFromContext(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), agentkit.KeyDeliverySessionID, agentkit.SessionID("slack:C001:t:17.9:u:U111"))
	ctx = context.WithValue(ctx, agentkit.KeyUserID, "U111")

	got := session.ChannelKeyFromContext(ctx)
	if got != "slack:C001" {
		t.Fatalf("ChannelKeyFromContext = %q, want slack:C001", got)
	}
}
