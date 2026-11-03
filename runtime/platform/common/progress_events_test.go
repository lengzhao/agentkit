package common

import (
	"github.com/lengzhao/agentkit"
	"testing"
)

func TestProgressEntryFromToolResult(t *testing.T) {
	for _, tc := range []struct {
		name    string
		audit   map[string]string
		success bool
	}{
		{"normal", nil, true}, {"denied", map[string]string{"decision": " deny "}, false}, {"failed", map[string]string{"status": "failed"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := ProgressEntryFromToolResult(agentkit.ToolResult{Content: " 中中文文 ", Audit: tc.audit}, 3)
			if item.Kind != ProgressEntryToolResult || item.Tool != "Tool" || item.Text != "中中文..." || item.Success == nil || *item.Success != tc.success {
				t.Fatalf("item=%+v", item)
			}
			expected := "completed"
			if !tc.success {
				expected = "failed"
			}
			if item.Status != expected {
				t.Fatalf("status=%q", item.Status)
			}
		})
	}
}
func TestProgressEntryFromToolResultKeepsInput(t *testing.T) {
	result := agentkit.ToolResult{Name: " Read ", Content: "  full output  "}
	item := ProgressEntryFromToolResult(result, 0)
	if item.Tool != "Read" || item.Text != "full output" || result.Name != " Read " || result.Content != "  full output  " {
		t.Fatalf("item=%+v result=%+v", item, result)
	}
}
