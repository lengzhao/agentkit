package loop

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestWithTurnContextSetsUserID(t *testing.T) {
	t.Parallel()

	ctx := withTurnContext(
		context.Background(),
		agentkit.SessionID("slack:C001:U456"),
		agentkit.AgentID("coder"),
		"slack",
		"U456",
		nil,
		nil,
	)

	userID, ok := ctx.Value(agentkit.KeyUserID).(string)
	if !ok || userID != "U456" {
		t.Fatalf("user id = %q, ok = %v", userID, ok)
	}
}

func TestWithTurnContextOmitsEmptyUserID(t *testing.T) {
	t.Parallel()

	ctx := withTurnContext(
		context.Background(),
		agentkit.SessionID("cli:default"),
		agentkit.AgentID("coder"),
		"cli",
		"",
		nil,
		nil,
	)

	if _, ok := ctx.Value(agentkit.KeyUserID).(string); ok {
		t.Fatal("expected no user id in context")
	}
}
