package telemetry_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

func TestFormatGenerationInputForExportTruncatesFieldsNotJSON(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 9000)
	raw := telemetry.FormatGenerationInputForExport(nil, []agentkit.ModelMessage{{
		Role:    "system",
		Content: []agentkit.ContentPart{{Type: "text", Text: long}},
	}}, 100, false)

	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("invalid JSON: %v\nraw=%q", err, raw)
	}
	messages, ok := got["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v", got["messages"])
	}
	first, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatal("first message not object")
	}
	content, _ := first["content"].(string)
	if len(content) > 100 {
		t.Fatalf("content not truncated: len=%d", len(content))
	}
	if !strings.Contains(content, "...[truncated]") {
		t.Fatalf("content = %q", content)
	}
}

func TestFormatGenerationInputForExportDeduplicatesSharedPrefix(t *testing.T) {
	t.Parallel()

	system := agentkit.ModelMessage{
		Role:    "system",
		Content: []agentkit.ContentPart{{Type: "text", Text: "you are helpful"}},
	}
	user := agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}},
	}
	assistant := agentkit.ModelMessage{
		Role:      "assistant",
		ToolCalls: []agentkit.ToolCall{{ID: "1", Name: "read", Input: []byte(`{"path":"a.txt"}`)}},
	}
	toolResult := agentkit.ModelMessage{
		Role:        "tool",
		ToolResults: []agentkit.ToolResult{{ID: "1", Name: "read", Content: "file body"}},
	}

	step1 := []agentkit.ModelMessage{system, user}
	first := telemetry.FormatGenerationInputForExport(nil, step1, 0, true)
	var firstPayload map[string]any
	if err := json.Unmarshal([]byte(first), &firstPayload); err != nil {
		t.Fatal(err)
	}
	if firstPayload["sharedPrefixMessages"] != float64(0) {
		t.Fatalf("first sharedPrefixMessages = %#v", firstPayload["sharedPrefixMessages"])
	}

	step2 := []agentkit.ModelMessage{system, user, assistant, toolResult}
	second := telemetry.FormatGenerationInputForExport(step1, step2, 0, true)
	var secondPayload map[string]any
	if err := json.Unmarshal([]byte(second), &secondPayload); err != nil {
		t.Fatal(err)
	}
	if secondPayload["sharedPrefixMessages"] != float64(2) {
		t.Fatalf("second sharedPrefixMessages = %#v", secondPayload["sharedPrefixMessages"])
	}
	secondMessages, _ := secondPayload["messages"].([]any)
	if len(secondMessages) != 2 {
		t.Fatalf("second messages = %#v", secondPayload["messages"])
	}
}

func TestExportMessagesIsFullFidelity(t *testing.T) {
	t.Parallel()

	raw := telemetry.ExportMessages([]agentkit.ModelMessage{{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("a", 10000)}},
	}})
	var got []map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	content, _ := got[0]["content"].(string)
	if len(content) != 10000 {
		t.Fatalf("ExportMessages should not truncate, len=%d", len(content))
	}
}