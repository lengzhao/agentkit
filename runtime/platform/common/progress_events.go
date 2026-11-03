package common

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// ProgressEntryFromToolResult is presentation-state normalization shared by
// platform UIs. It does not mutate the tool result or perform transport work.
func ProgressEntryFromToolResult(result agentkit.ToolResult, maxRunes int) ProgressCardEntry {
	name := strings.TrimSpace(result.Name)
	if name == "" {
		name = "Tool"
	}
	text := strings.TrimSpace(result.Content)
	if runes := []rune(text); maxRunes > 0 && len(runes) > maxRunes {
		text = string(runes[:maxRunes]) + "..."
	}
	success := strings.TrimSpace(result.Audit["decision"]) != "deny" && strings.TrimSpace(result.Audit["status"]) != "failed"
	status := "completed"
	if !success {
		status = "failed"
	}
	return ProgressCardEntry{Kind: ProgressEntryToolResult, Tool: name, Text: text, Status: status, Success: &success}
}
