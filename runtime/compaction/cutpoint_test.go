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

// 事故回归：当前轮的触发 user message 必须留在保留尾部。修复前 keepRecent
// 预算被近期巨大的 tool result 占满时，切点会落在触发消息之后，用户指令被
// 摘要掉，模型在轮次中段丢失任务（trace 中出现多步"重建用户意图"的螺旋）。
func TestFindCutPointPinsInflightTurnTrigger(t *testing.T) {
	t.Parallel()

	big := stringsRepeat("x", 4000)
	indexed := []capscompaction.IndexedMessage{
		{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "old question"}}}, Seq: 1, IsTurnStart: true},
		{Message: agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "old reply"}}}, Seq: 2},
		{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "publish the wiki now"}}}, Seq: 3, IsTurnStart: true},
		{Message: agentkit.ModelMessage{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "c1", Name: "bash"}}}, Seq: 4},
		{Message: agentkit.ModelMessage{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "c1", Name: "bash", Content: big}}}, Seq: 5},
		{Message: agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: big}}}, Seq: 6},
	}
	// 小 keep 预算：不 pin 时切点落在 index 4+（触发消息被摘要）。
	cut := rtcompaction.FindCutPoint(indexed, 0, len(indexed), 50)
	if cut.FirstKeptIndex > 3 {
		t.Fatalf("FirstKeptIndex = %d, want <= 3: in-flight turn trigger must stay in the retained tail", cut.FirstKeptIndex)
	}
	if cut.IsSplitTurn {
		t.Fatal("pinned cut at the turn trigger must not be treated as a split turn")
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}
