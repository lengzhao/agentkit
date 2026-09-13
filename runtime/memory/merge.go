package memory

import (
	"strings"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
)

// MergeMemoryAdd integrates one fact into existing entries (exact + substring rules).
func MergeMemoryAdd(entries []MemoryEntry, content string) ([]MemoryEntry, capmemory.AddOutcome) {
	content = normalizeMemoryText(content)
	if content == "" {
		return entries, capmemory.AddOutcomeDuplicate
	}
	out := append([]MemoryEntry{}, entries...)
	for i, e := range out {
		ex := normalizeMemoryText(e.Content)
		if ex == content {
			return entries, capmemory.AddOutcomeDuplicate
		}
		if strings.Contains(ex, content) {
			return entries, capmemory.AddOutcomeDuplicate
		}
		if strings.Contains(content, ex) {
			out[i] = MemoryEntry{Content: content}
			return out, capmemory.AddOutcomeReplaced
		}
	}
	out = append(out, MemoryEntry{Content: content})
	return out, capmemory.AddOutcomeAdded
}

// DedupeMemoryEntries merges a list in order (later entries can replace earlier subsets).
func DedupeMemoryEntries(entries []MemoryEntry) []MemoryEntry {
	var out []MemoryEntry
	for _, e := range entries {
		text := normalizeMemoryText(e.Content)
		if text == "" {
			continue
		}
		out, _ = MergeMemoryAdd(out, text)
	}
	return out
}

func normalizeMemoryText(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
