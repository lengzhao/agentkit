package telemetry_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

func TestExportMessagesIncludesToolResults(t *testing.T) {
	t.Parallel()

	raw := telemetry.ExportMessages([]agentkit.ModelMessage{{
		Role:        "tool",
		ToolResults: []agentkit.ToolResult{{ID: "1", Name: "read", Content: "file body"}},
	}})
	if !strings.Contains(raw, `"toolResults"`) || !strings.Contains(raw, "file body") {
		t.Fatalf("ExportMessages = %q", raw)
	}
}

func TestFormatGenerationInputSkipsWholePayloadTruncation(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 20000)
	raw := telemetry.FormatGenerationInputForExport(nil, []agentkit.ModelMessage{{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: long}},
	}}, 25000, false)

	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("generation export must stay valid JSON: %v", err)
	}
}
