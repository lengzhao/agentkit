package compaction

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestDropOldestMessagesToFitStripsLeadingTool(t *testing.T) {
	t.Parallel()

	msgs := []agentkit.ModelMessage{
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "c1", Name: "read"}}},
		{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "c1", Name: "read", Content: "x"}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("z", 2000)}}},
	}
	out, dropped := dropOldestMessagesToFit(msgs, 500)
	if !dropped {
		t.Fatal("expected drop")
	}
	if len(out) > 0 && len(out[0].ToolResults) > 0 {
		t.Fatalf("retained head must not be tool-only orphan: %#v", out[0])
	}
}

// 事故回归：预算再紧也不能丢掉最后一条 user message（当前轮触发指令），
// 否则 agent 在轮次中段丢失任务。
func TestDropOldestMessagesToFitNeverDropsLastUserMessage(t *testing.T) {
	t.Parallel()

	msgs := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "publish the wiki now"}}},
		{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("a", 2000)}}},
		{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "c1", Name: "bash", Content: strings.Repeat("b", 2000)}}},
	}
	out, _ := dropOldestMessagesToFit(msgs, 500)
	if len(out) == 0 || out[0].Role != "user" {
		t.Fatalf("last user message must be pinned as retained head, got %#v", out)
	}
	if out[0].Content[0].Text != "publish the wiki now" {
		t.Fatalf("pinned message content = %q", out[0].Content[0].Text)
	}
}
