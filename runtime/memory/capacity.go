package memory

import (
	"fmt"
	"strings"
)

// MemoryAtCapacityError is returned when a write would exceed the char limit.
type MemoryAtCapacityError struct {
	Used    int
	Limit   int
	Adding  int
	Entries []MemoryEntry
}

func (e *MemoryAtCapacityError) Error() string {
	if e == nil {
		return "memory at capacity"
	}
	return fmt.Sprintf(
		"Memory at %d/%d chars. Adding this entry (%d chars) would exceed the limit. Consolidate now: use 'replace' to merge overlapping entries into shorter ones or 'remove' stale entries, then retry — all in this turn.",
		e.Used, e.Limit, e.Adding,
	)
}

// FormatMemoryUsage returns a compact usage string such as "1474/2200".
func FormatMemoryUsage(used, limit int) string {
	if limit <= 0 {
		return fmt.Sprintf("%d", used)
	}
	return fmt.Sprintf("%d/%d", used, limit)
}

// EntryContents returns trimmed entry bodies for tool errors.
func EntryContents(entries []MemoryEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		c := strings.TrimSpace(e.Content)
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}
