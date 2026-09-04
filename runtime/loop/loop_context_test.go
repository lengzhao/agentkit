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
		agentkit.SessionID("slack:C001"),
		agentkit.SessionID("slack:C001:t:111.0:u:U456"),
		"",
		agentkit.AgentID("coder"),
		"slack",
		"U456",
		nil,
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
		agentkit.SessionID("cli:default"),
		"",
		agentkit.AgentID("coder"),
		"cli",
		"",
		nil,
		nil,
		nil,
	)

	if _, ok := ctx.Value(agentkit.KeyUserID).(string); ok {
		t.Fatal("expected no user id in context")
	}
}

func TestWithTurnContextSetsDeliverySessionID(t *testing.T) {
	t.Parallel()

	ctx := withTurnContext(
		context.Background(),
		agentkit.SessionID("slack:C001"),
		agentkit.SessionID("slack:C001:t:111.0:u:U456"),
		"",
		agentkit.AgentID("coder"),
		"slack",
		"U456",
		nil,
		nil,
		nil,
	)

	delivery, ok := ctx.Value(agentkit.KeyDeliverySessionID).(agentkit.SessionID)
	if !ok || delivery != "slack:C001:t:111.0:u:U456" {
		t.Fatalf("delivery session id = %q, ok = %v", delivery, ok)
	}
	effective, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	if effective != "slack:C001" {
		t.Fatalf("effective session id = %q", effective)
	}
}

func TestWithTurnContextSetsStoreSessionID(t *testing.T) {
	t.Parallel()

	ctx := withTurnContext(
		context.Background(),
		agentkit.SessionID("slack:C001"),
		agentkit.SessionID("slack:C001:t:111.0:u:U456"),
		agentkit.SessionID("slack:C001:new:20260829"),
		agentkit.AgentID("coder"),
		"slack",
		"U456",
		nil,
		nil,
		nil,
	)

	store, ok := ctx.Value(agentkit.KeyStoreSessionID).(agentkit.SessionID)
	if !ok || store != "slack:C001:new:20260829" {
		t.Fatalf("store session id = %q, ok = %v", store, ok)
	}
	history, ok := ctx.Value(agentkit.KeyHistorySessionID).(agentkit.SessionID)
	if !ok || history != store {
		t.Fatalf("history session id = %q, ok = %v", history, ok)
	}
}
