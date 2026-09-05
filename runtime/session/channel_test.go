package session_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestWorkspaceFromContextUsesDeliveryRoute(t *testing.T) {
	t.Parallel()

	ctx := session.ContextWithDeliveryRoute(context.Background(), "slack", agentkit.SessionID("slack:C001:t:17.9:u:U111"))
	env := session.EnvelopeFromContext(ctx)
	env.Actor.UserID = "U111"
	ctx = session.ApplyEnvelopeToContext(ctx, env)

	got := session.WorkspaceFromContext(ctx)
	if got != "slack:C001" {
		t.Fatalf("WorkspaceFromContext = %q, want slack:C001", got)
	}
}
