package sessbind_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestResolveAgentIDPriority(t *testing.T) {
	t.Parallel()

	ws, err := rtworkspace.New(rtworkspace.Config{
		Global: t.TempDir(),
		Local:  t.TempDir(),
		Scope:  "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "sess"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	const entry = agentkit.SessionID("cli:agent-prio")
	const sid = agentkit.SessionID("cli:agent-prio:child")

	if err := store.SetAgentBind(ctx, sid, "session-agent"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetChannelAgentBind(ctx, store, ws, entry, "channel-agent"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalAgentBind(ctx, ws, "global-agent"); err != nil {
		t.Fatal(err)
	}

	got, sess, ch, glob, err := sessbind.ResolveAgentID(ctx, store, ws, sid, entry, "request-agent")
	if err != nil {
		t.Fatal(err)
	}
	if got != "session-agent" || sess != "session-agent" || ch != "channel-agent" || glob != "global-agent" {
		t.Fatalf("session wins: got=%q sess=%q channel=%q glob=%q", got, sess, ch, glob)
	}

	if err := store.SetAgentBind(ctx, sid, ""); err != nil {
		t.Fatal(err)
	}
	got, sess, ch, glob, err = sessbind.ResolveAgentID(ctx, store, ws, sid, entry, "request-agent")
	if err != nil {
		t.Fatal(err)
	}
	if got != "channel-agent" || sess != "" || ch != "channel-agent" || glob != "global-agent" {
		t.Fatalf("channel wins: got=%q sess=%q channel=%q glob=%q", got, sess, ch, glob)
	}

	if err := sessbind.SetChannelAgentBind(ctx, store, ws, entry, ""); err != nil {
		t.Fatal(err)
	}
	got, _, ch, glob, err = sessbind.ResolveAgentID(ctx, store, ws, sid, entry, "request-agent")
	if err != nil {
		t.Fatal(err)
	}
	if got != "global-agent" || ch != "" || glob != "global-agent" {
		t.Fatalf("global wins: got=%q channel=%q glob=%q", got, ch, glob)
	}

	if err := sessbind.SetGlobalAgentBind(ctx, ws, ""); err != nil {
		t.Fatal(err)
	}
	got, _, _, _, err = sessbind.ResolveAgentID(ctx, store, ws, sid, entry, "request-agent")
	if err != nil || got != "request-agent" {
		t.Fatalf("request wins: got=%q err=%v", got, err)
	}
}
