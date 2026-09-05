package learning

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
	workspaceruntime "github.com/lengzhao/agentkit/runtime/workspace"
)

type stubSessionStore struct{}

func (stubSessionStore) Get(context.Context, agentkit.SessionID) (agentkit.Session, error) {
	return nil, nil
}

func TestMemoryStoreAddAndLoad(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/memory.md"
	store := NewMemoryStore(path, 200)
	if err := store.Add("prefers concise answers", "test"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Content != "prefers concise answers" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestMemoryStoreRejectsSecret(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(t.TempDir()+"/memory.md", 200)
	if err := store.Add("api_key=supersecret", "test"); err == nil {
		t.Fatal("expected secret rejection")
	}
}

func TestLearnCommandMemory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws, err := workspaceruntime.New(workspaceruntime.Config{Global: root, Local: root, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{}, Deps{Workspace: ws, SessionStore: stubSessionStore{}})
	if err != nil {
		t.Fatal(err)
	}
	cmd := svc.Commands()[0]
	out, err := cmd.CommandExec(context.Background(), "memory likes Go tests")
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("expected confirmation")
	}
	show, err := cmd.CommandExec(context.Background(), "show")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show, "likes Go tests") {
		t.Fatalf("show = %q", show)
	}
}

func TestLearnCommandMemorySucceedsWhenDreamingSignalCannotBeWritten(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws, err := workspaceruntime.New(workspaceruntime.Config{Global: root, Local: root, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	// Blocks memory/dreaming/state.json while leaving memory.md writable.
	if err := os.MkdirAll(filepath.Join(root, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "memory", "dreaming"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{}, Deps{Workspace: ws, SessionStore: stubSessionStore{}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Commands()[0].CommandExec(context.Background(), "memory likes Go tests")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "personal memory updated") || !strings.Contains(out, "warning: dreaming signal not recorded") {
		t.Fatalf("out = %q", out)
	}
	show, err := svc.Commands()[0].CommandExec(context.Background(), "show")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show, "likes Go tests") {
		t.Fatalf("show = %q", show)
	}
}

func TestLearnCommandHelp(t *testing.T) {
	t.Parallel()

	svc, err := New(Config{}, Deps{
		Workspace:    rtworkspace.Static(t.TempDir()),
		SessionStore: stubSessionStore{},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Commands()[0].CommandExec(context.Background(), "help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "/learn dream") {
		t.Fatalf("help = %q", out)
	}
}

func TestSummarizeSessionUserMessages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{
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
		if err := session.AppendMessage(ctx, sess, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
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
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{}, Deps{Workspace: ws, SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:default", Workspace: "cli:default"})
	sess, err := store.Get(ctx, agentkit.SessionID("cli:default"))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "remember I prefer YAML configs"}},
	}); err != nil {
		t.Fatal(err)
	}

	out, err := svc.Commands()[0].CommandExec(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("expected confirmation")
	}
	show, err := svc.Commands()[0].CommandExec(ctx, "show")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show, "remember I prefer YAML configs") {
		t.Fatalf("show = %q", show)
	}
}
