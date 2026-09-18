package learning

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	workspaceruntime "github.com/lengzhao/agentkit/runtime/workspace"
)

type stubSessionStore struct{}

func (stubSessionStore) Get(context.Context, agentkit.SessionID) (agentkit.Session, error) {
	return nil, nil
}

func TestLearnCommandHelp(t *testing.T) {
	t.Parallel()

	ws := rtworkspace.Static(t.TempDir())
	svc, err := New(Config{}, Deps{
		Workspace:    ws,
		SessionStore: stubSessionStore{},
		Memory:       newTestMemoryStub(ws),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{"", "help"} {
		out, err := svc.Commands()[0].CommandExec(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "/learn dream") {
			t.Fatalf("args=%q help = %q", args, out)
		}
		if strings.Contains(out, "/learn memory") {
			t.Fatalf("memory commands moved to /memory: %q", out)
		}
	}
}

func TestSummarizeSessionUserMessages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{
		Workspace: rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sess, err := store.Get(ctx, agentkit.SessionID("cli:default"))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"/learn help", "prefers Go", "likes tests"} {
		if err := sessstore.AppendMessage(ctx, sess, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := SummarizeSessionUserMessages(ctx, store, sess.ID(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "/learn") {
		t.Fatalf("slash commands should be skipped: %q", got)
	}
	if !strings.Contains(got, "prefers Go") || !strings.Contains(got, "likes tests") {
		t.Fatalf("summary = %q", got)
	}
}

func TestLearnCommandSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws, err := workspaceruntime.New(workspaceruntime.Config{Global: dir, Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{}, Deps{Workspace: ws, SessionStore: store, Memory: newTestMemoryStub(ws)})
	if err != nil {
		t.Fatal(err)
	}

	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:default", Workspace: "cli:default"})
	sess, err := store.Get(ctx, agentkit.SessionID("cli:default"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sessstore.AppendMessage(ctx, sess, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "remember I prefer YAML configs"}},
	}); err != nil {
		t.Fatal(err)
	}

	out, err := svc.Commands()[0].CommandExec(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dreaming signals") {
		t.Fatalf("session out = %q", out)
	}
}

func TestLearnCommandSessionDreamingBlockedPath(t *testing.T) {
	dir := t.TempDir()
	ws, err := workspaceruntime.New(workspaceruntime.Config{Global: dir, Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory", "dreaming"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{}, Deps{Workspace: ws, SessionStore: stubSessionStore{}, Memory: newTestMemoryStub(ws)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Commands()[0].CommandExec(context.Background(), "session")
	if err == nil {
		t.Fatal("expected dreaming signal write to fail when dreaming path blocked")
	}
}
