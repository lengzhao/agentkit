package sessctx_test

import (
	"context"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
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
