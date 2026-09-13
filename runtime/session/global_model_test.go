package session_test

import (
	"context"
	"os"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestGlobalModelBind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	ctx := context.Background()

	if got, err := session.GlobalModelBind(ctx, ws, "assistant"); err != nil || got != "" {
		t.Fatalf("initial = %q err=%v", got, err)
	}
	if err := session.SetGlobalModelBind(ctx, ws, "assistant", "global-model"); err != nil {
		t.Fatal(err)
	}
	if got, err := session.GlobalModelBind(ctx, ws, "assistant"); err != nil || got != "global-model" {
		t.Fatalf("bind = %q err=%v", got, err)
	}

	path, err := ws.Resolve(ctx, "global:runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing at %s: %v", path, err)
	}

	if err := session.SetGlobalModelBind(ctx, ws, "assistant", ""); err != nil {
		t.Fatal(err)
	}
	if got, err := session.GlobalModelBind(ctx, ws, "assistant"); err != nil || got != "" {
		t.Fatalf("cleared = %q err=%v", got, err)
	}
}

func TestResolveEffectiveModelPriority(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	ctx := context.Background()

	mem, err := session.NewMemory(session.MemoryConfig{ID: "sess"})
	if err != nil {
		t.Fatal(err)
	}
	store := session.NewStaticStore(mem)

	const sid = agentkit.SessionID("cli:prio")
	if err := store.SetModelBind(ctx, sid, "session-model"); err != nil {
		t.Fatal(err)
	}
	if err := session.SetGlobalModelBind(ctx, ws, "assistant", "global-model"); err != nil {
		t.Fatal(err)
	}

	got, sess, glob, err := session.ResolveEffectiveModel(ctx, store, ws, sid, "assistant", "default-model")
	if err != nil {
		t.Fatal(err)
	}
	if got != "session-model" || sess != "session-model" || glob != "" {
		t.Fatalf("got=%q sess=%q glob=%q", got, sess, glob)
	}

	if err := store.SetModelBind(ctx, sid, ""); err != nil {
		t.Fatal(err)
	}
	got, sess, glob, err = session.ResolveEffectiveModel(ctx, store, ws, sid, "assistant", "default-model")
	if err != nil {
		t.Fatal(err)
	}
	if got != "global-model" || sess != "" || glob != "global-model" {
		t.Fatalf("got=%q sess=%q glob=%q", got, sess, glob)
	}

	if err := session.SetGlobalModelBind(ctx, ws, "assistant", ""); err != nil {
		t.Fatal(err)
	}
	got, _, _, err = session.ResolveEffectiveModel(ctx, store, ws, sid, "assistant", "default-model")
	if err != nil || got != "default-model" {
		t.Fatalf("default got=%q err=%v", got, err)
	}
}
