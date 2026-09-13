package memory

import (
	"fmt"
	"strings"
)

// Staged action kinds for pending.json (empty Action = legacy encoded Content).
const (
	StagedActionAdd     = "add"
	StagedActionReplace = "replace"
	StagedActionRemove  = "remove"
)

const (
	legacyStagedRemovePrefix = "remove:"
	legacyStagedReplaceSep   = " => "
)

// StagedPendingSummary is a human-readable line for /memory pending.
func StagedPendingSummary(e StagedMemory) string {
	switch strings.TrimSpace(e.Action) {
	case StagedActionRemove:
		return fmt.Sprintf("remove match %q", strings.TrimSpace(e.OldText))
	case StagedActionReplace:
		return fmt.Sprintf("replace %q → %s", strings.TrimSpace(e.OldText), truncateStagedDisplay(e.Content, 80))
	case StagedActionAdd:
		return fmt.Sprintf("add %s", truncateStagedDisplay(e.Content, 120))
	default:
		return LegacyStagedContentSummary(e.Content)
	}
}

// LegacyStagedContentSummary decodes pre-structured pending payloads.
func LegacyStagedContentSummary(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, legacyStagedRemovePrefix) {
		old := strings.TrimSpace(strings.TrimPrefix(content, legacyStagedRemovePrefix))
		return fmt.Sprintf("remove match %q", old)
	}
	if i := strings.Index(content, legacyStagedReplaceSep); i >= 0 {
		old := strings.TrimSpace(content[:i])
		newText := strings.TrimSpace(content[i+len(legacyStagedReplaceSep):])
		return fmt.Sprintf("replace %q → %s", old, truncateStagedDisplay(newText, 80))
	}
	return fmt.Sprintf("add %s", truncateStagedDisplay(content, 120))
}

// LegacyStagedKind classifies a legacy Content-only staged row.
func LegacyStagedKind(content string) (action, oldText, newContent string) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, legacyStagedRemovePrefix) {
		return StagedActionRemove, strings.TrimSpace(strings.TrimPrefix(content, legacyStagedRemovePrefix)), ""
	}
	if i := strings.Index(content, legacyStagedReplaceSep); i >= 0 {
		return StagedActionReplace, strings.TrimSpace(content[:i]), strings.TrimSpace(content[i+len(legacyStagedReplaceSep):])
	}
	return StagedActionAdd, "", content
}

func truncateStagedDisplay(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
