package compaction_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
)

func TestFindCutPointNeverCutsAtToolResult(t *testing.T) {
	t.Parallel()

	indexed := []capscompaction.IndexedMessage{
		{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "go"}}}, Seq: 1, IsTurnStart: true},
		{Message: agentkit.ModelMessage{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "1", Name: "read"}}}, Seq: 2},
		{Message: agentkit.ModelMessage{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "1", Name: "read", Content: "ok"}}}, Seq: 3},
		{Message: agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "done"}}}, Seq: 4},
	}
	cut := rtcompaction.FindCutPoint(indexed, 0, len(indexed), 5)
	if indexed[cut.FirstKeptIndex].Message.Role == "tool" {
		t.Fatal("must not cut at tool result index")
	}
}

func TestPrepareRetainsTailVerbatim(t *testing.T) {
	t.Parallel()

	indexed := []capscompaction.IndexedMessage{
		{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: stringsRepeat("old ", 200)}}}, Seq: 1, IsTurnStart: true},
		{Message: agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: stringsRepeat("old reply ", 200)}}}, Seq: 2},
		{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "recent"}}}, Seq: 3, IsTurnStart: true},
		{Message: agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "recent reply"}}}, Seq: 4},
	}
	prep := rtcompaction.Prepare(indexed, 0, 50, "", 0)
	if prep == nil {
		t.Fatal("expected preparation")
	}
	if len(prep.RetainedTail) < 2 {
		t.Fatalf("expected retained tail, got %d", len(prep.RetainedTail))
	}
	last := prep.RetainedTail[len(prep.RetainedTail)-1].Content[0].Text
	if last != "recent reply" {
		t.Fatalf("last retained = %q", last)
	}
}

// 事故回归：存储形态很小（attachment_ref）但 metadata 记录了入站真实大小的消息，
// 必须按记录的大小参与 keepRecentTokens 累加，否则切点永远落在 0、Prepare 返回 nil。
func TestFindCutPointUsesRecordedLogicalChars(t *testing.T) {
	t.Parallel()

	small := agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}}
	indexed := []capscompaction.IndexedMessage{
		{Message: small, Seq: 1, IsTurnStart: true},
		{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "attachment_ref", Source: "upload/a.png"}}}, Seq: 2, IsTurnStart: true, LogicalChars: 2_000_000},
		{Message: small, Seq: 3, IsTurnStart: true},
		{Message: small, Seq: 4, IsTurnStart: true},
	}
	cut := rtcompaction.FindCutPoint(indexed, 0, len(indexed), 100)
	// 回退累加在巨型消息（index 1）处达到预算 → 从它开始保留；
	// 修复前估算看不到它，cutIndex 会退化为 0（无可摘要内容 → Prepare nil）。
	if cut.FirstKeptIndex != 1 {
		t.Fatalf("FirstKeptIndex = %d, want 1 (recorded logical size must count toward keep budget)", cut.FirstKeptIndex)
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}
