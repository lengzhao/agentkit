package compaction_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
)

func TestFindCutPointNeverCutsAtToolResult(t *testing.T) {
	t.Parallel()

	indexed := []compaction.IndexedMessage{
		{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "go"}}}, Seq: 1, IsTurnStart: true},
		{Message: agentkit.ModelMessage{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "1", Name: "read"}}}, Seq: 2},
		{Message: agentkit.ModelMessage{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "1", Name: "read", Content: "ok"}}}, Seq: 3},
		{Message: agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "done"}}}, Seq: 4},
	}
	cut := compaction.FindCutPoint(indexed, 0, len(indexed), 5)
	if indexed[cut.FirstKeptIndex].Message.Role == "tool" {
		t.Fatal("must not cut at tool result index")
	}
}

func TestPrepareRetainsTailVerbatim(t *testing.T) {
	t.Parallel()

	indexed := []compaction.IndexedMessage{
		{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: stringsRepeat("old ", 200)}}}, Seq: 1, IsTurnStart: true},
		{Message: agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: stringsRepeat("old reply ", 200)}}}, Seq: 2},
		{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "recent"}}}, Seq: 3, IsTurnStart: true},
		{Message: agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "recent reply"}}}, Seq: 4},
	}
	prep := compaction.Prepare(indexed, 0, 50, "", 0)
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

func stringsRepeat(s string, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}
