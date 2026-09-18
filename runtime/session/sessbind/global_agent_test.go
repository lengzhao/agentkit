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

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	ctx := context.Background()

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "sess"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	const sid = agentkit.SessionID("cli:agent-prio")

	if err := store.SetAgentBind(ctx, sid, "session-agent"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalAgentBind(ctx, ws, "global-agent"); err != nil {
		t.Fatal(err)
	}

	got, sess, glob, err := sessbind.ResolveAgentID(ctx, store, ws, sid, "request-agent")
	if err != nil {
		t.Fatal(err)
	}
	if got != "session-agent" || sess != "session-agent" || glob != "" {
		t.Fatalf("session wins: got=%q sess=%q glob=%q", got, sess, glob)
	}

	if err := store.SetAgentBind(ctx, sid, ""); err != nil {
		t.Fatal(err)
	}
	got, sess, glob, err = sessbind.ResolveAgentID(ctx, store, ws, sid, "request-agent")
	if err != nil {
		t.Fatal(err)
	}
	if got != "global-agent" || sess != "" || glob != "global-agent" {
		t.Fatalf("global wins: got=%q sess=%q glob=%q", got, sess, glob)
	}

	if err := sessbind.SetGlobalAgentBind(ctx, ws, ""); err != nil {
		t.Fatal(err)
	}
	got, _, _, err = sessbind.ResolveAgentID(ctx, store, ws, sid, "request-agent")
	if err != nil || got != "request-agent" {
		t.Fatalf("request wins: got=%q err=%v", got, err)
	}
}
