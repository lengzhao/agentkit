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

// NormalizeStagedEntry fills Action/OldText/Content from legacy Content encoding when needed.
func NormalizeStagedEntry(e StagedMemory) StagedMemory {
	if strings.TrimSpace(e.Action) != "" {
		return e
	}
	action, oldText, content := LegacyStagedKind(e.Content)
	e.Action = action
	e.OldText = oldText
	if action == StagedActionAdd {
		e.Content = content
	} else if action == StagedActionReplace {
		e.Content = content
	}
	return e
}

// StagedPendingSummary is a human-readable line for /memory pending.
func StagedPendingSummary(e StagedMemory) string {
	e = NormalizeStagedEntry(e)
	return stagedActionSummary(e.Action, e.OldText, e.Content)
}

// LegacyStagedContentSummary decodes pre-structured pending payloads.
func LegacyStagedContentSummary(content string) string {
	action, oldText, newContent := LegacyStagedKind(content)
	return stagedActionSummary(action, oldText, newContent)
}

func stagedActionSummary(action, oldText, content string) string {
	switch strings.TrimSpace(action) {
	case StagedActionRemove:
		return fmt.Sprintf("remove match %q", strings.TrimSpace(oldText))
	case StagedActionReplace:
		return fmt.Sprintf("replace %q → %s", strings.TrimSpace(oldText), truncateStagedDisplay(content, 80))
	case StagedActionAdd:
		return fmt.Sprintf("add %s", truncateStagedDisplay(content, 120))
	default:
		return fmt.Sprintf("add %s", truncateStagedDisplay(content, 120))
	}
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
