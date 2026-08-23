package hookbeforestep_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/plugins/compactionprune"
	"github.com/lengzhao/agentkit/plugins/hookbeforestep"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestCompactCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID("cli:default")
	sess, err := store.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", 200)
	if err := session.AppendMessage(context.Background(), sess, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(context.Background(), sess, "coder", agentkit.EventToolResult, agentkit.ModelMessage{
		Role:    "tool",
		Content: []agentkit.ContentPart{{Type: "text", Text: long}},
	}); err != nil {
		t.Fatal(err)
	}

	prune, err := compactionprune.New(compactionprune.Config{MaxToolResultBytes: 20})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := hookbeforestep.New(hookbeforestep.Config{}, hookbeforestep.Deps{
		SessionStore: store,
		Services:     []compaction.Service{prune},
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

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, sessionID)
	out, err := commands[0].CommandExec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "applied 1 service") {
		t.Fatalf("unexpected output: %q", out)
	}
}
