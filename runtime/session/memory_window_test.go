package session_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestCompactionTrimsMemoryButKeepsDeriveMessages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mem, err := session.NewMemory(session.MemoryConfig{ID: "trim"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := session.AppendMessage(ctx, mem, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "old"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.AppendMessage(ctx, mem, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "recent"}},
	}); err != nil {
		t.Fatal(err)
	}
	beforeSeq, err := session.LatestSeq(ctx, mem)
	if err != nil {
		t.Fatal(err)
	}
	summary := agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "summary"}},
	}
	data := compaction.EventData{
		BeforeSeq:    beforeSeq - 1,
		FirstKeptSeq: beforeSeq,
		RetainedTail: []agentkit.ModelMessage{{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "recent"}},
		}},
		Kind:    compaction.KindSummary,
		Summary: summary,
	}
	if err := session.AppendCompaction(ctx, mem, "coder", data); err != nil {
		t.Fatal(err)
	}

	events, err := mem.Read(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) > 3 {
		t.Fatalf("expected trimmed memory events, got %d", len(events))
	}
	msgs, err := mem.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("derived messages = %d, want summary + retained", len(msgs))
	}
	if msgs[0].Content[0].Text != summary.Content[0].Text {
		t.Fatalf("summary = %q", msgs[0].Content[0].Text)
	}
	if msgs[1].Content[0].Text != "recent" {
		t.Fatalf("retained = %q", msgs[1].Content[0].Text)
	}
}

func TestJSONLTailLoadAndDiskReadBack(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "tail.jsonl")
	writeJSONLEvent := func(seq agentkit.EventSeq, text string) {
		t.Helper()
		raw, err := json.Marshal(agentkit.SessionEvent{
			Seq:       seq,
			SessionID: "tail",
			AgentID:   "coder",
			Type:      agentkit.EventUserMessage,
			Data: mustMarshal(agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: text}},
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	for i := 1; i <= 20; i++ {
		writeJSONLEvent(agentkit.EventSeq(i), "old")
	}
	compactionRaw, err := json.Marshal(agentkit.SessionEvent{
		Seq:       21,
		SessionID: "tail",
		AgentID:   "coder",
		Type:      agentkit.EventCompaction,
		Data: mustMarshal(compaction.EventData{
			BeforeSeq: 18,
			Kind:      compaction.KindSummary,
			Summary: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "[Conversation summary]\nsummary"}},
			},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(compactionRaw, '\n')); err != nil {
		t.Fatal(err)
	}
	f.Close()
	for i := 22; i <= 24; i++ {
		writeJSONLEvent(agentkit.EventSeq(i), "recent")
	}

	sess, err := session.NewJSONL(session.JSONLConfig{
		Path:            path,
		ID:              "tail",
		MaxLoadedEvents: 4,
	})
	if err != nil {
		t.Fatal(err)
	}

	memEvents, err := sess.Read(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(memEvents) >= 20 {
		t.Fatalf("expected tail-loaded memory, got %d events", len(memEvents))
	}
	if seq := sess.(interface{ LatestSeq() agentkit.EventSeq }).LatestSeq(); seq != 24 {
		t.Fatalf("latest seq = %d, want 24", seq)
	}

	full, err := sess.Read(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 24 {
		t.Fatalf("disk read back events = %d, want 24", len(full))
	}
}

func TestStorePassesMaxLoadedEvents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "store_tail.jsonl")
	for i := 1; i <= 10; i++ {
		raw, _ := json.Marshal(agentkit.SessionEvent{
			Seq:       agentkit.EventSeq(i),
			SessionID: "store",
			AgentID:   "coder",
			Type:      agentkit.EventUserMessage,
			Data:      mustMarshal(agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "x"}}}),
		})
		f, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		_, _ = f.Write(append(raw, '\n'))
		f.Close()
	}

	store, err := session.NewStore(session.StoreConfig{
		Dir:               ".",
		MaxLoadedEvents:   3,
		MaxCachedSessions: 1,
	}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(context.Background(), agentkit.SessionID("store"))
	if err != nil {
		t.Fatal(err)
	}
	events, err := sess.Read(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) > 3 {
		t.Fatalf("loaded %d events into memory, want tail <= 3", len(events))
	}
}

func mustMarshal(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}
