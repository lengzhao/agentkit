package sessbind_test

import (
	"context"
	"os"
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
	env := rctx.ResolveEnvelope(rctx.WithDeliveryRoute(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: agentkit.RouteRef{Instance: "slack"}, Actor: agentkit.ActorRef{UserID: "U1"}}}, rctx.SessionRoute("slack", string(delivery))), rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
	return rctx.ApplyEnvelopeToContext(context.Background(), env)
}

func TestChannelAgentBindIsolatedFromInitialSession(t *testing.T) {
	t.Parallel()
	for _, sessionFirst := range []bool{true, false} {
		name := "channel-first"
		if sessionFirst {
			name = "session-first"
		}
		t.Run(name, func(t *testing.T) {
			ctx := slashCtx("C001")
			entry := rctx.ActiveEntryKeyFromContext(ctx)
			ws := rtworkspace.Static(t.TempDir())
			store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
			if err != nil {
				t.Fatal(err)
			}
			setSession := func() error { return sessbind.SetSessionAgentBind(ctx, store, entry, "session-agent") }
			setChannel := func() error { return sessbind.SetChannelAgentBind(ctx, store, ws, entry, "channel-agent") }
			steps := []func() error{setChannel, setSession}
			if sessionFirst {
				steps = []func() error{setSession, setChannel}
			}
			for _, step := range steps {
				if err := step(); err != nil {
					t.Fatal(err)
				}
			}
			// Reopen the file store so this also checks persistence.
			store, err = sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
			if err != nil {
				t.Fatal(err)
			}
			got, session, channel, _, err := sessbind.ResolveAgentID(ctx, store, ws, entry, entry, "")
			if err != nil || got != "session-agent" || session != "session-agent" || channel != "channel-agent" {
				t.Fatalf("initial session: effective=%q session=%q channel=%q err=%v", got, session, channel, err)
			}
			got, session, channel, _, err = sessbind.ResolveAgentID(ctx, store, ws, entry+":new", entry, "")
			if err != nil || got != "channel-agent" || session != "" || channel != "channel-agent" {
				t.Fatalf("new session: effective=%q session=%q channel=%q err=%v", got, session, channel, err)
			}
			if err := sessbind.SetChannelAgentBind(ctx, store, ws, entry, ""); err != nil {
				t.Fatal(err)
			}
			got, session, channel, _, err = sessbind.ResolveAgentID(ctx, store, ws, entry, entry, "")
			if err != nil || got != "session-agent" || session != "session-agent" || channel != "" {
				t.Fatalf("clear channel: effective=%q session=%q channel=%q err=%v", got, session, channel, err)
			}
			if err := setChannel(); err != nil {
				t.Fatal(err)
			}
			if err := sessbind.SetSessionAgentBind(ctx, store, entry, ""); err != nil {
				t.Fatal(err)
			}
			got, session, channel, _, err = sessbind.ResolveAgentID(ctx, store, ws, entry, entry, "")
			if err != nil || got != "channel-agent" || session != "" || channel != "channel-agent" {
				t.Fatalf("clear session: effective=%q session=%q channel=%q err=%v", got, session, channel, err)
			}
		})
	}
}

func TestSessionAgentBindDoesNotBecomeChannelDefault(t *testing.T) {
	t.Parallel()
	ctx := slashCtx("C001")
	entry := rctx.ActiveEntryKeyFromContext(ctx)
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: entry})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	if err := sessbind.SetSessionAgentBind(ctx, store, entry, "session-agent"); err != nil {
		t.Fatal(err)
	}
	got, session, channel, _, err := sessbind.ResolveAgentID(ctx, store, nil, entry+":new", entry, "")
	if err != nil || got != "" || session != "" || channel != "" {
		t.Fatalf("new session without channel bind: effective=%q session=%q channel=%q err=%v", got, session, channel, err)
	}
}

func TestChannelAgentBindPreservesGlobalRuntime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws := rtworkspace.Static(t.TempDir())
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "slack:C001"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	if err := sessbind.SetGlobalAgentBind(ctx, ws, "global-agent"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalModelBind(ctx, ws, "global-agent", "global-model"); err != nil {
		t.Fatal(err)
	}
	if got, err := sessbind.ChannelAgentBind(ctx, store, ws, "slack:C001"); err != nil || got != "" {
		t.Errorf("global must not be read as channel: got=%q err=%v", got, err)
	}
	for _, agent := range []agentkit.AgentID{"channel-agent", ""} {
		if err := sessbind.SetChannelAgentBind(ctx, store, ws, "slack:C001", agent); err != nil {
			t.Fatal(err)
		}
		if got, err := sessbind.GlobalAgentBind(ctx, ws); err != nil || got != "global-agent" {
			t.Errorf("global agent after channel=%q: got=%q err=%v", agent, got, err)
		}
		if got, err := sessbind.GlobalModelBind(ctx, ws, "global-agent"); err != nil || got != "global-model" {
			t.Errorf("global model after channel=%q: got=%q err=%v", agent, got, err)
		}
	}
}

func TestChannelAgentBindLegacyLocalRuntime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := rtworkspace.New(rtworkspace.Config{Global: t.TempDir(), Local: t.TempDir(), Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	path, err := ws.Resolve(ctx, "local:runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"agentId":"legacy-agent"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "slack:C001"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	if got, err := sessbind.ChannelAgentBind(ctx, store, ws, "slack:C001"); err != nil || got != "legacy-agent" {
		t.Fatalf("legacy channel: got=%q err=%v", got, err)
	}
	if err := sessbind.SetChannelAgentBind(ctx, store, ws, "slack:C001", "channel-agent"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetChannelAgentBind(ctx, store, ws, "slack:C001", ""); err != nil {
		t.Fatal(err)
	}
	if got, err := sessbind.ChannelAgentBind(ctx, store, ws, "slack:C001"); err != nil || got != "" {
		t.Fatalf("cleared channel must not revive legacy: got=%q err=%v", got, err)
	}
}
