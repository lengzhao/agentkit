package commandcompact_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/command"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/plugins/commandcompact"
	"github.com/lengzhao/agentkit/plugins/compactionprune"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestCompactCommandAppliesPrune(t *testing.T) {
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
	handler, err := commandcompact.NewCompact(commandcompact.CompactConfig{AgentID: "coder"}, commandcompact.CompactDeps{
		SessionStore: store,
		Services:     []compaction.Service{prune},
	})
	if err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	_, err = handler.Handle(context.Background(), command.Request{
		Name:      "compact",
		SessionID: sessionID,
		ErrOut:    &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "applied 1 service") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}
