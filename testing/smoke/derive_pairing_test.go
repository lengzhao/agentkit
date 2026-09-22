package smoke_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// SMK-041: derive repairToolPairing must not emit provider-invalid history
// (leading tool after compaction tail break or crash orphan tool/result).
func TestSmokeDeriveReplayPairingAfterOrphanToolEvent(t *testing.T) {
	t.Parallel()

	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{AgentID: agentkit.AgentID("assistant")})
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "smk-derive-pairing-orphan"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendToolResult(ctx, sess, "assistant", agentkit.ToolResult{
		ID: "call-orphan", Name: "read", Content: "leftover without assistant",
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "continue"}},
	}); err != nil {
		t.Fatal(err)
	}
	agenttest.AssertDeriveMessagesReplayPairing(t, sess, ctx)
}

func TestSmokeDeriveReplayPairingCompactionBrokenTail(t *testing.T) {
	t.Parallel()

	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{AgentID: agentkit.AgentID("assistant")})
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "smk-derive-pairing-compact"})
	if err != nil {
		t.Fatal(err)
	}
	compData, err := json.Marshal(capscompaction.EventData{
		Kind:         capscompaction.KindSummary,
		FirstKeptSeq: 2,
		Summary: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "[Conversation summary]\nok"}},
		},
		RetainedTail: []agentkit.ModelMessage{
			{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "call-x", Name: "read", Content: "tail"}}},
			{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "follow up"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Append(ctx, agentkit.SessionEvent{
		AgentID: "assistant",
		Type:    agentkit.EventCompaction,
		Data:    compData,
	}); err != nil {
		t.Fatal(err)
	}
	agenttest.AssertDeriveMessagesReplayPairing(t, sess, ctx)
}
