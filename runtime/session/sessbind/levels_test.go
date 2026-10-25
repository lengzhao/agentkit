package sessbind_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"

	cw "github.com/lengzhao/agentkit/cap/workspace"
)

func threadStore(t *testing.T, id agentkit.SessionID) (*sessstore.StaticStore, cw.Service) {
	t.Helper()
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	ws, err := rtworkspace.New(rtworkspace.Config{Global: t.TempDir(), Local: t.TempDir(), Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	return sessstore.NewStaticStore(mem), ws
}

// larkThreadCtx builds a thread-scoped turn context for chat oc_d95 / topic.
func larkThreadCtx(topic string) context.Context {
	delivery := rctx.BuildDeliverySessionID("lark", "oc_d95", topic, "ou_user")
	env := rctx.ResolveEnvelope(rctx.WithDeliveryRoute(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: agentkit.RouteRef{Instance: "lark"}, Actor: agentkit.ActorRef{UserID: "ou_user"}}}, rctx.SessionRoute("lark", string(delivery))), rctx.RoutePolicyForPlatform("lark", rctx.DefaultRoutePolicy(agentkit.SessionScopeThread)))
	env = rctx.WithMetadataScope(env, agentkit.SessionScopeThread)
	return rctx.ApplyEnvelopeToContext(context.Background(), env)
}

// Every level's key is computed from one place, with the documented granularity.
func TestBindKeysGranularity(t *testing.T) {
	t.Parallel()
	keys := rctx.BindKeysFromContext(larkThreadCtx("om_topicA"))
	if keys.Channel != "lark:oc_d95" {
		t.Fatalf("channel = %q", keys.Channel)
	}
	if keys.Thread != "lark:oc_d95:t:om_topicA" {
		t.Fatalf("thread = %q", keys.Thread)
	}
	if keys.User != "lark:oc_d95:u:ou_user" {
		t.Fatalf("user = %q", keys.User)
	}
	// Same chat, different topic: channel key is stable, thread key differs.
	other := rctx.BindKeysFromContext(larkThreadCtx("om_topicB"))
	if other.Channel != keys.Channel {
		t.Fatalf("channel key fragmented: %q vs %q", keys.Channel, other.Channel)
	}
	if other.Thread == keys.Thread {
		t.Fatalf("thread key must differ per topic")
	}
}

// /agent -c and /model -c both bind at the channel and are visible across
// threads — the two kinds now share one stack.
func TestUnifiedStackChannelBindCrossesThreads(t *testing.T) {
	t.Parallel()
	store, ws := threadStore(t, "lark:oc_d95")

	stack := sessbind.StackFromContext(larkThreadCtx("om_topicA"), ws)
	if err := stack.Set(context.Background(), store, sessbind.AgentBinding{}, sessbind.LevelChannel, "worker"); err != nil {
		t.Fatal(err)
	}
	if err := stack.Set(context.Background(), store, sessbind.ModelBinding{}, sessbind.LevelChannel, "gpt-x"); err != nil {
		t.Fatal(err)
	}

	for _, topic := range []string{"om_topicA", "om_topicB"} {
		ctx := larkThreadCtx(topic)
		gotAgent, _, _, _, err := sessbind.ResolveAgentID(ctx, store, ws, "", "", "")
		if err != nil || gotAgent != "worker" {
			t.Fatalf("topic %s agent=%q err=%v", topic, gotAgent, err)
		}
		gotModel, winner, _, err := sessbind.ResolveModelBinding(ctx, store, ws, "", "", "")
		if err != nil || gotModel != "gpt-x" || winner != sessbind.LevelChannel {
			t.Fatalf("topic %s model=%q winner=%v err=%v", topic, gotModel, winner, err)
		}
	}
}

// Priority is identical for both kinds: session beats channel beats global.
func TestUnifiedStackPrioritySameForBothKinds(t *testing.T) {
	t.Parallel()
	store, ws := threadStore(t, "lark:oc_d95")
	ctx := larkThreadCtx("om_topicA")
	stack := sessbind.StackFromContext(ctx, ws).WithSession("lark:oc_d95:session")

	for _, tc := range []struct {
		kind  sessbind.Binding
		level sessbind.Level
		val   string
	}{
		{sessbind.AgentBinding{}, sessbind.LevelGlobal, "g"},
		{sessbind.AgentBinding{}, sessbind.LevelChannel, "c"},
		{sessbind.AgentBinding{}, sessbind.LevelSession, "s"},
		// Global model is per-agent, so it needs the agent id.
		{sessbind.ModelBinding{AgentID: "assistant"}, sessbind.LevelGlobal, "g"},
		{sessbind.ModelBinding{}, sessbind.LevelChannel, "c"},
		{sessbind.ModelBinding{}, sessbind.LevelSession, "s"},
	} {
		if err := stack.Set(ctx, store, tc.kind, tc.level, tc.val); err != nil {
			t.Fatalf("%s %s: %v", tc.kind.Key(), tc.level, err)
		}
	}

	if got, winner, _, _ := stack.EffectiveWithLevel(ctx, store, sessbind.AgentBinding{}); got != "s" || winner != sessbind.LevelSession {
		t.Fatalf("agent got=%q winner=%v", got, winner)
	}
	if got, winner, _, _ := stack.EffectiveWithLevel(ctx, store, sessbind.ModelBinding{AgentID: "assistant"}); got != "s" || winner != sessbind.LevelSession {
		t.Fatalf("model got=%q winner=%v", got, winner)
	}

	// Drop the session bind: channel must win for both.
	if err := store.SetAgentBind(ctx, "lark:oc_d95:session", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetModelBind(ctx, "lark:oc_d95:session", ""); err != nil {
		t.Fatal(err)
	}
	if got, winner, _, _ := stack.EffectiveWithLevel(ctx, store, sessbind.AgentBinding{}); got != "c" || winner != sessbind.LevelChannel {
		t.Fatalf("agent got=%q winner=%v", got, winner)
	}
	if got, winner, _, _ := stack.EffectiveWithLevel(ctx, store, sessbind.ModelBinding{AgentID: "assistant"}); got != "c" || winner != sessbind.LevelChannel {
		t.Fatalf("model got=%q winner=%v", got, winner)
	}
}
