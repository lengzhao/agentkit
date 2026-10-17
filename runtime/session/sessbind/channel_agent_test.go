package sessbind_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestChannelAgentBindUsesEntryKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}

	const entryA = agentkit.SessionID("slack:C001")
	const entryB = agentkit.SessionID("slack:C002")
	ctxA := slashCtx("C001")
	ctxB := slashCtx("C002")

	if err := sessbind.SetChannelAgentBind(ctxA, store, ws, entryA, "worker"); err != nil {
		t.Fatal(err)
	}
	got, err := sessbind.ChannelAgentBind(ctxA, store, ws, entryA)
	if err != nil || got != "worker" {
		t.Fatalf("channel A = %q err=%v", got, err)
	}
	got, err = sessbind.ChannelAgentBind(ctxB, store, ws, entryB)
	if err != nil || got != "" {
		t.Fatalf("channel B = %q err=%v", got, err)
	}

	if err := sessbind.SetChannelAgentBind(ctxA, store, ws, entryA, ""); err != nil {
		t.Fatal(err)
	}
	got, err = sessbind.ChannelAgentBind(ctxA, store, ws, entryA)
	if err != nil || got != "" {
		t.Fatalf("cleared = %q err=%v", got, err)
	}
}

func slashCtx(channel string) context.Context {
	delivery := rctx.BuildDeliverySessionID("slack", channel, "", "U1")
	env := rctx.ResolveEnvelope(rctx.WithDeliveryRoute(agentkit.MessageEvent{
		PlatformID: "slack",
		UserID:     "U1",
	}, rctx.SessionRoute("slack", string(delivery))), rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
	return rctx.ApplyEnvelopeToContext(context.Background(), env)
}
