package learning

import (
	"fmt"
	"strings"
)

const (
	// DefaultMemoryRoot is the workspace-relative directory for memory.md, aligned with prompt/section/memory.
	DefaultMemoryRoot = "work"
	DefaultMemoryFile = "memory.md"
	DefaultCharLimit  = 2200
)

// MemoryRelPath returns workspace-relative path to memory.md.
func MemoryRelPath(root, file string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultMemoryRoot
	}
	file = strings.TrimSpace(file)
	if file == "" {
		file = DefaultMemoryFile
	}
	if root == "." {
		return file
	}
	return root + "/" + file
}

// FormatHelp returns /learn usage text.
func FormatHelp() string {
	return `Usage:
  /learn                         show personal memory
  /learn memory <text>           append a memory entry immediately
  /learn remove <text>           remove entries containing <text>
  /learn session                 learn from current session user messages
  /learn help                    show this help

Notes:
  - Memory is stored in memory.md (same file prompt/section/memory injects)
  - Secrets-looking content is rejected
  - Do not put a standalone "§" line in memory text; it is used as an entry delimiter`
}

// FormatMemory renders memory entries for display.
func FormatMemory(entries []MemoryEntry, used, limit int) string {
	if len(entries) == 0 {
		return "no personal memory yet"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "personal memory [%d/%d chars, %d entries]:\n", used, limit, len(entries))
	for i, e := range entries {
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(e.Content))
	}
	return strings.TrimRight(b.String(), "\n")
}
