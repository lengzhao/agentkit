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
