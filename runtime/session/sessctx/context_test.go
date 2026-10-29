package sessctx_test

import (
	"context"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareSharesCwdAndHistoryBoundary(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	other := t.TempDir()
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "session"})
	store := sessstore.NewStaticStore(mem)
	ws := rtworkspace.Static(root)
	base := rctx.WithConversation(context.Background(), "session")
	appendUser := func(text string) {
		t.Helper()
		if err := sessevents.Default.AppendMessage(base, mem, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: text}}}); err != nil {
			t.Fatal(err)
		}
	}
	appendUser("old history")
	if err := store.SetCwdBind(base, "session", project); err != nil {
		t.Fatal(err)
	}
	ctx, err := sessctx.Prepare(base, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	if got := workspace.WorkingDirectory(ctx); got != project {
		t.Fatalf("cwd=%q", got)
	}
	appendUser("project history")
	if err := store.SetCwdBind(base, "session", other); err != nil {
		t.Fatal(err)
	}
	// Existing turn keeps its snapshot; only a new turn prepares the changed binding.
	again, err := sessctx.Prepare(ctx, store, ws)
	if err != nil || workspace.WorkingDirectory(again) != project {
		t.Fatalf("snapshot changed: %v", err)
	}
	next, err := sessctx.Prepare(base, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.WorkingDirectory(next) != other {
		t.Fatal("new turn missed binding")
	}
	messages, err := mem.DeriveMessages(base)
	if err != nil || len(messages) != 0 {
		t.Fatalf("old project history visible: %+v %v", messages, err)
	}
	appendUser("new project")
	messages, err = mem.DeriveMessages(base)
	if err != nil || len(messages) != 1 || messages[0].Content[0].Text != "new project" {
		t.Fatalf("derived: %+v %v", messages, err)
	}
	before, _ := mem.Read(base, 0)
	if _, err := sessctx.Prepare(base, store, ws); err != nil {
		t.Fatal(err)
	}
	after, _ := mem.Read(base, 0)
	if len(before) != len(after) {
		t.Fatal("same cwd created redundant context event")
	}
	if err := store.SetCwdBind(base, "session", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sessctx.Prepare(base, store, ws); err != nil {
		t.Fatal(err)
	}
	messages, _ = mem.DeriveMessages(base)
	if len(messages) != 0 {
		t.Fatal("reset replayed project history")
	}
	all, _ := mem.Read(base, 0)
	if len(all) <= len(messages) {
		t.Fatal("audit history lost")
	}
}

func TestNextTurnRefreshesPendingBinding(t *testing.T) {
	root := t.TempDir()
	first := t.TempDir()
	second := t.TempDir()
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "session"})
	store := sessstore.NewStaticStore(mem)
	ws := rtworkspace.Static(root)
	base := rctx.WithConversation(context.Background(), "session")
	_ = store.SetCwdBind(base, "session", first)
	ctx, err := sessctx.Prepare(base, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.SetCwdBind(base, "session", second)
	next, err := sessctx.NextTurn(ctx)
	if err != nil || workspace.WorkingDirectory(next) != second {
		t.Fatalf("next turn directory: %q %v", workspace.WorkingDirectory(next), err)
	}
	if workspace.WorkingDirectory(ctx) != first {
		t.Fatal("previous turn snapshot changed")
	}
}

func TestResolvePredictsContextWithoutChangingHistory(t *testing.T) {
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "session"})
	store := sessstore.NewStaticStore(mem)
	ws := rtworkspace.Static(t.TempDir())
	base := rctx.WithConversation(context.Background(), "session")
	_ = store.SetCwdBind(base, "session", t.TempDir())
	first, err := sessctx.Prepare(base, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.SetCwdBind(base, "session", t.TempDir())
	before, _ := mem.Read(base, 0)
	pending, err := sessctx.Resolve(base, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := mem.Read(base, 0)
	if len(before) != len(after) {
		t.Fatal("read-only configuration changed model context")
	}
	next, err := sessctx.Prepare(base, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := rctx.ExecutionFromContext(next)
	old, _ := rctx.ExecutionFromContext(first)
	if pending.Epoch == "" || pending.Epoch != actual.Epoch || actual.Epoch == old.Epoch {
		t.Fatalf("unstable context identity: old=%s predicted=%s actual=%s", old.Epoch, pending.Epoch, actual.Epoch)
	}
}

func TestChildSessionBindingOverridesInheritedCwd(t *testing.T) {
	root := t.TempDir()
	parentDir := t.TempDir()
	childDir := t.TempDir()
	ws := rtworkspace.Static(root)
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := rctx.WithConversation(context.Background(), "child")
	ctx = rctx.WithInheritedWorkingDirectory(ctx, parentDir)
	prepared, err := sessctx.Prepare(ctx, store, ws)
	if err != nil || workspace.WorkingDirectory(prepared) != parentDir {
		t.Fatalf("inherited: %q %v", workspace.WorkingDirectory(prepared), err)
	}
	if err := store.(agentkit.SessionCwdStore).SetCwdBind(ctx, "child", childDir); err != nil {
		t.Fatal(err)
	}
	prepared, err = sessctx.Prepare(ctx, store, ws)
	if err != nil || workspace.WorkingDirectory(prepared) != childDir {
		t.Fatalf("explicit child: %q %v", workspace.WorkingDirectory(prepared), err)
	}
}

// A bound directory can disappear after it was persisted (repo moved, temp dir
// cleaned, another host). That must not fail every later turn, and the
// configuration path must keep working so the binding can be cleared.
func TestUnusableCwdBindingDoesNotBrickSession(t *testing.T) {
	root := t.TempDir()
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "session"})
	store := sessstore.NewStaticStore(mem)
	ws := rtworkspace.Static(root)
	base := rctx.WithConversation(context.Background(), "session")

	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCwdBind(base, "session", gone); err != nil {
		t.Fatal(err)
	}
	if _, err := sessctx.Prepare(base, store, ws); err != nil {
		t.Fatalf("prepare while cwd exists: %v", err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	ctx, err := sessctx.Prepare(base, store, ws)
	if err != nil {
		t.Fatalf("prepare failed on unusable binding: %v", err)
	}
	if got := workspace.WorkingDirectory(ctx); got != "" {
		t.Fatalf("unusable binding still applied: %q", got)
	}
	// /cwd reads through Resolve; it must not fail either.
	if _, err := sessctx.Resolve(base, store, ws); err != nil {
		t.Fatalf("resolve failed on unusable binding: %v", err)
	}
	// Clearing the binding works and normal turns resume.
	if err := store.SetCwdBind(base, "session", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sessctx.Prepare(base, store, ws); err != nil {
		t.Fatalf("prepare after reset: %v", err)
	}
}

// An explicit setter still rejects a path that does not exist.
func TestSetCwdRejectsMissingDirectory(t *testing.T) {
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "session"})
	store := sessstore.NewStaticStore(mem)
	ws := rtworkspace.Static(t.TempDir())
	base := rctx.WithConversation(context.Background(), "session")
	if _, err := sessctx.Prepare(base, store, ws); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := sessbind.ValidateCwd(base, ws, missing); err == nil {
		t.Fatal("ValidateCwd accepted a missing directory")
	}
}

func TestWorkspacePolicyDefaultsAndCwdReset(t *testing.T) {
	ws, err := rtworkspace.NewTenant(rtworkspace.TenantConfig{Global: t.TempDir(), LocalBase: t.TempDir(), WorkspaceScope: "thread"})
	if err != nil {
		t.Fatal(err)
	}
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "S1"})
	store := sessstore.NewStaticStore(mem)
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", Instance: "bot1", ChannelID: "C1", ThreadID: "T1"})
	env := rctx.ResolveEnvelope(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: route}}, rctx.DefaultRoutePolicy(agentkit.SessionScopeThread))
	env.Conversation = "S1"
	base := rctx.ApplyEnvelopeToContext(context.Background(), env)
	expected, err := ws.(workspace.DefaultDirectoryResolver).DefaultWorkingDirectory(base)
	if err != nil {
		t.Fatal(err)
	}
	first, err := sessctx.Prepare(base, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.WorkingDirectory(first) != expected {
		t.Fatalf("default = %s want %s", workspace.WorkingDirectory(first), expected)
	}
	if info, err := os.Stat(expected); err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}
	override := t.TempDir()
	if err = store.SetCwdBind(base, "S1", override); err != nil {
		t.Fatal(err)
	}
	second, err := sessctx.NextTurn(first)
	if err != nil || workspace.WorkingDirectory(second) != override {
		t.Fatalf("override failed: %v", err)
	}
	if workspace.WorkingDirectory(first) != expected {
		t.Fatal("current turn changed")
	}
	if err = store.SetCwdBind(base, "S1", ""); err != nil {
		t.Fatal(err)
	}
	reset, err := sessctx.NextTurn(second)
	if err != nil || workspace.WorkingDirectory(reset) != expected {
		t.Fatalf("reset failed: %v", err)
	}
	// Work directory policy must not change the underlying state root.
	before, _ := ws.Resolve(base, "local:sessions")
	after, _ := ws.Resolve(reset, "local:sessions")
	if before != after {
		t.Fatalf("state root moved: %s -> %s", before, after)
	}
}

func TestScopedWorkspaceRejectsInvalidCwdWithoutWritingElsewhere(t *testing.T) {
	ws, err := rtworkspace.NewTenant(rtworkspace.TenantConfig{Global: t.TempDir(), LocalBase: t.TempDir(), WorkspaceScope: "global"})
	if err != nil {
		t.Fatal(err)
	}
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "S1"})
	store := sessstore.NewStaticStore(mem)
	base := rctx.WithConversation(context.Background(), "S1")
	if err = store.SetCwdBind(base, "S1", filepath.Join(t.TempDir(), "gone")); err != nil {
		t.Fatal(err)
	}
	if _, err = sessctx.Prepare(base, store, ws); err == nil {
		t.Fatal("invalid binding silently fell back")
	}
	if _, err = sessctx.CwdCommand(store, ws).CommandExec(base, "reset"); err != nil {
		t.Fatalf("cannot recover: %v", err)
	}
	if _, err = sessctx.Prepare(base, store, ws); err != nil {
		t.Fatal(err)
	}
}
