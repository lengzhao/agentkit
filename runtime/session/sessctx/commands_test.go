package sessctx_test

import (
	"context"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCwdCommandValidationAndActiveSession(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project with spaces")
	if err := os.Mkdir(project, 0755); err != nil {
		t.Fatal(err)
	}
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "cli:test"})
	store := sessstore.NewStaticStore(mem)
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:test"})
	if err := store.SetActiveSession(ctx, "cli:test", "cli:new"); err != nil {
		t.Fatal(err)
	}
	cmd := sessctx.CwdCommand(store, rtworkspace.Static(root))
	if _, err := cmd.CommandExec(ctx, "use "+project); err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CommandExec(ctx, "")
	if err != nil || !strings.Contains(out, project) || !strings.Contains(out, "session") {
		t.Fatalf("%s %v", out, err)
	}
	for _, arg := range []string{"use missing", "use " + filepath.Join(root, "file"), "-g use " + project} {
		_ = os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0600)
		if _, err := cmd.CommandExec(ctx, arg); err == nil {
			t.Fatalf("accepted %q", arg)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing directory created: %v", err)
	}
	if _, err := cmd.CommandExec(ctx, "reset"); err != nil {
		t.Fatal(err)
	}
	out, err = cmd.CommandExec(ctx, "")
	if err != nil || strings.Contains(out, project) {
		t.Fatalf("reset: %s %v", out, err)
	}
}

func TestCwdChannelInheritanceAndReset(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	override := filepath.Join(root, "override")
	for _, path := range []string{project, override} {
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "lark:chat"})
	store := sessstore.NewStaticStore(mem)
	cmd := sessctx.CwdCommand(store, rtworkspace.Static(root))
	turn := func(channel, topic, session string) context.Context {
		delivery := rctx.BuildDeliverySessionID("lark", channel, topic, "user")
		env := rctx.ResolveEnvelope(rctx.WithDeliveryRoute(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: agentkit.RouteRef{Instance: "lark"}, Actor: agentkit.ActorRef{UserID: "user"}}}, rctx.SessionRoute("lark", string(delivery))), rctx.DefaultRoutePolicy(agentkit.SessionScopeThread))
		env.Conversation = session
		return rctx.ApplyEnvelopeToContext(context.Background(), env)
	}
	first := turn("chat", "topic-a", "session-a")
	second := turn("chat", "topic-b", "session-b")
	other := turn("other", "topic-c", "session-c")
	exec := func(ctx context.Context, args string) string {
		t.Helper()
		out, err := cmd.CommandExec(ctx, args)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	exec(first, "-c use "+project)
	if out := exec(second, ""); !strings.Contains(out, project) {
		t.Fatalf("channel not inherited: %s", out)
	}
	if out := exec(other, ""); strings.Contains(out, project) {
		t.Fatalf("cross-channel leak: %s", out)
	}
	exec(first, "use "+override)
	if out := exec(first, ""); !strings.Contains(out, "Cwd: "+override) {
		t.Fatalf("no session override: %s", out)
	}
	exec(first, "reset")
	if out := exec(first, ""); !strings.Contains(out, "Cwd: "+project) {
		t.Fatalf("reset did not fall back: %s", out)
	}
	exec(second, "--channel reset")
	if out := exec(first, ""); strings.Contains(out, project) {
		t.Fatalf("channel reset failed: %s", out)
	}
}
