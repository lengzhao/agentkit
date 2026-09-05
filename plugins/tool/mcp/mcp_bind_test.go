package mcp

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/bind"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

func TestParseBinds(t *testing.T) {
	t.Parallel()

	binds, err := parseBinds(map[string]rawBind{
		"X-User-Id": {From: "ctx:user_id", In: "header"},
		"traceId":   {From: "ctx:turn_id", In: "meta", Name: "trace_id"},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(binds) != 2 {
		t.Fatalf("binds = %d, want 2", len(binds))
	}
}

func TestParseBindsRejectsInvalid(t *testing.T) {
	t.Parallel()

	_, err := parseBinds(map[string]rawBind{
		"uid": {From: "user_id", In: "header"},
	})
	if err == nil {
		t.Fatal("expected error for missing ctx: prefix")
	}

	_, err = parseBinds(map[string]rawBind{
		"uid": {From: "ctx:user_id", In: "query"},
	})
	if err == nil {
		t.Fatal("expected error for unsupported in")
	}
}

func TestResolveCtxValue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = func() context.Context { env := session.EnvelopeFromContext(ctx); env.Actor.UserID = "u-42"; return session.ApplyEnvelopeToContext(ctx, env) }()
	ctx = session.WithAgentID(ctx, agentkit.AgentID("coder"))
	ctx = func() context.Context { env := session.EnvelopeFromContext(ctx); env.Conversation = "slack:C001:t:1"; env.Workspace = "slack:C001"; return session.ApplyEnvelopeToContext(ctx, env) }()
	ctx = context.WithValue(ctx, agentkit.KeyTurnID, "turn-abc")
	ctx = func() context.Context { env := session.EnvelopeFromContext(ctx); env.Metadata = map[string]any{"channel": "general"}; return session.ApplyEnvelopeToContext(ctx, env) }()

	cases := []struct {
		from string
		want string
	}{
		{"ctx:user_id", "u-42"},
		{"ctx:agent_id", "coder"},
		{"ctx:session_id", "slack:C001:t:1"},
		{"ctx:turn_id", "turn-abc"},
		{"ctx:metadata.channel", "general"},
		{"ctx:tenant", "slack:C001"},
	}
	for _, tc := range cases {
		got, err := bind.ResolveCtxValue(ctx, tc.from)
		if err != nil {
			t.Fatalf("%s: %v", tc.from, err)
		}
		if got != tc.want {
			t.Fatalf("%s = %q, want %q", tc.from, got, tc.want)
		}
	}
}

func TestResolveCtxValueTurnIDFromTelemetry(t *testing.T) {
	t.Parallel()

	ctx := telemetry.WithTurnID(context.Background(), "telemetry-turn")
	got, err := bind.ResolveCtxValue(ctx, "ctx:turn_id")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "telemetry-turn" {
		t.Fatalf("turn_id = %q", got)
	}
}

func TestCallMetaFromBindsSkipsEmpty(t *testing.T) {
	t.Parallel()

	meta := callMetaFromBinds(context.Background(), []bindConfig{
		{Key: "uid", From: "ctx:user_id", In: "meta"},
		{Key: "trace", From: "ctx:turn_id", In: "meta", Name: "trace_id"},
	})
	if meta != nil {
		t.Fatalf("meta = %#v, want nil when all binds empty", meta)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeyTurnID, "trace-1")
	meta = callMetaFromBinds(ctx, []bindConfig{
		{Key: "uid", From: "ctx:user_id", In: "meta"},
		{Key: "trace", From: "ctx:turn_id", In: "meta", Name: "trace_id"},
	})
	if meta == nil || meta.AdditionalFields["trace_id"] != "trace-1" {
		t.Fatalf("meta = %#v", meta)
	}
	if _, ok := meta.AdditionalFields["uid"]; ok {
		t.Fatal("empty uid bind should be skipped")
	}
}

func TestHeaderBindFunc(t *testing.T) {
	t.Parallel()

	fn := headerBindFunc([]bindConfig{
		{Key: "X-User-Id", From: "ctx:user_id", In: "header"},
		{Key: "trace", From: "ctx:turn_id", In: "meta"},
	})
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Actor: agentkit.ActorRef{UserID: "u-7"}})
	headers := fn(ctx)
	if headers["X-User-Id"] != "u-7" {
		t.Fatalf("headers = %#v", headers)
	}
}

func TestParseConfigFileWithBind(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "mcpServers": {
    "remote": {
      "url": "http://127.0.0.1:8080/mcp",
      "type": "sse",
      "bind": {
        "X-User-Id": { "from": "ctx:user_id", "in": "header" },
        "traceId": { "from": "ctx:turn_id", "in": "meta", "name": "trace_id" }
      }
    }
  }
}`)
	servers, err := parseConfigFile("/tmp/mcp.json", raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %d", len(servers))
	}
	if len(servers[0].Binds) != 2 {
		t.Fatalf("binds = %d", len(servers[0].Binds))
	}
}
