package bind

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestResolveCtxValueRequiresPrefix(t *testing.T) {
	_, err := ResolveCtxValue(context.Background(), "session_id")
	if err == nil {
		t.Fatal("expected error without ctx: prefix")
	}
}

func TestResolveCtxValueFromTurnContext(t *testing.T) {
	ctx := agenttest.TurnContext("chat-api:default:conv", "my-agent")
	ctx = rctx.WithRoute(ctx, agentkit.RouteRef{Platform: "chat-api"})
	ctx = rctx.ApplyEnvelopeToContext(ctx, rctx.EnvelopeFromContext(ctx))
	env := rctx.EnvelopeFromContext(ctx)
	env.Actor.UserID = "user-42"
	env.Workspace = "tenant-a"
	ctx = rctx.ApplyEnvelopeToContext(ctx, env)
	ctx = rctx.WithContextMetadata(ctx, map[string]any{"channel": "general", "count": 3})

	cases := map[string]string{
		"ctx:session_id":          "chat-api:default:conv",
		"ctx:agent_id":            "my-agent",
		"ctx:platform_id":         "chat-api",
		"ctx:user_id":             "user-42",
		"ctx:tenant":              "tenant-a",
		"ctx:metadata.channel":    "general",
		"ctx:metadata.count":      "3",
	}
	for from, want := range cases {
		got, err := ResolveCtxValue(ctx, from)
		if err != nil {
			t.Fatalf("%s: %v", from, err)
		}
		if got != want {
			t.Fatalf("%s = %q, want %q", from, got, want)
		}
	}
}

func TestResolveCtxValueMissingReturnsEmpty(t *testing.T) {
	got, err := ResolveCtxValue(context.Background(), "ctx:session_id")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestResolveCtxValueUnsupportedKey(t *testing.T) {
	_, err := ResolveCtxValue(context.Background(), "ctx:unknown_key")
	if err == nil {
		t.Fatal("expected error for unsupported key")
	}
}

func TestResolveCtxValueMetadataKeyRequired(t *testing.T) {
	_, err := ResolveCtxValue(context.Background(), "ctx:metadata.")
	if err == nil {
		t.Fatal("expected error for empty metadata key")
	}
}

func TestStringifyValue(t *testing.T) {
	if stringifyValue(true) != "true" {
		t.Fatal("bool")
	}
	if stringifyValue(int64(7)) != "7" {
		t.Fatal("int64")
	}
}
