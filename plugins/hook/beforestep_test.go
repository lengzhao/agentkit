package hook_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/plugins/compaction"
	"github.com/lengzhao/agentkit/plugins/hook"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestCompactCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID("cli:default")
	sess, err := store.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", 200)
	if err := sessevents.Default.AppendMessage(context.Background(), sess, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendToolResult(context.Background(), sess, "coder", agentkit.ToolResult{
		ID:      "call-1",
		Name:    "read",
		Content: long,
	}); err != nil {
		t.Fatal(err)
	}

	prune, err := compaction.NewPrune(compaction.PruneConfig{MaxToolResultBytes: 20})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := hook.New(hook.Config{ContributeCommands: true}, hook.Deps{
		SessionStore: store,
		Services:     []capcompaction.Service{prune},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmdProvider, ok := provider.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("expected hook provider to implement CommandProvider")
	}
	commands := cmdProvider.Commands()
	if len(commands) != 1 || commands[0].Name() != "compact" {
		t.Fatalf("unexpected commands: %+v", commands)
	}

	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(sessionID), Workspace: string(sessionID)})
	out, err := commands[0].CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "applied 1 service") {
		t.Fatalf("unexpected output: %q", out)
	}
}
