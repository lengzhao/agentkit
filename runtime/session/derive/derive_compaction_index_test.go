package derive_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
)

// 事故回归：sanitize 把巨型 part 剥成 attachment_ref 后，事件 metadata 里记录的
// 入站真实大小（logical_chars）必须随 indexed 消息传递给压缩切点计算。
func TestIndexMessagesForCompactionPopulatesLogicalChars(t *testing.T) {
	t.Parallel()

	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{AgentID: "assistant"})
	stripped := agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "attachment_ref", Source: "upload/big.log", MIME: "text/plain"}},
	}
	small := agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "继续"}},
	}
	events := []agentkit.SessionEvent{
		{
			Seq:     1,
			AgentID: "assistant",
			Type:    agentkit.EventUserMessage,
			Data:    mustMarshalMsg(t, stripped),
			// AppendMessage 在 sanitize 前记录的真实大小。
			Metadata: map[string]any{derive.MetadataLogicalChars: 3_000_000},
		},
		{
			Seq:     2,
			AgentID: "assistant",
			Type:    agentkit.EventUserMessage,
			Data:    mustMarshalMsg(t, small),
		},
	}

	indexed := derive.IndexMessagesForCompaction(ctx, events)
	if len(indexed) != 2 {
		t.Fatalf("indexed len = %d, want 2", len(indexed))
	}
	if indexed[0].LogicalChars != 3_000_000 {
		t.Fatalf("indexed[0].LogicalChars = %d, want recorded 3000000", indexed[0].LogicalChars)
	}
	want := derive.EstimateLogicalChars(small)
	if indexed[1].LogicalChars != want {
		t.Fatalf("indexed[1].LogicalChars = %d, want fallback EstimateLogicalChars %d", indexed[1].LogicalChars, want)
	}
}

func mustMarshalMsg(t *testing.T, msg agentkit.ModelMessage) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
