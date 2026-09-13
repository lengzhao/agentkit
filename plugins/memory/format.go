package memory

import (
	"fmt"
	"strings"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
)

func FormatMemory(entries []capmemory.MemoryEntry, used, limit int) string {
	if len(entries) == 0 {
		return "no personal memory yet"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "personal memory (memory.md) [%d/%d chars, %d entries]:\n", used, limit, len(entries))
	for i, e := range entries {
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(e.Content))
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatHelp() string {
	return `Usage:
  /memory                         show this help
  /memory show                    show memory.md (not staged pending)
  /memory add <text>              append a memory entry immediately
  /memory remove <text>           remove entries containing <text>
  /memory pending                 list staged memory awaiting approval
  /memory approve <id|all>        apply staged memory to memory.md
  /memory reject <id|all>         drop staged memory
  /memory policy                  show background-review memory write policy
  /memory policy approve|auto     stage or auto-write from review
  /memory policy reset            use config defaults again
  /memory help                    show this help

Notes:
  - Long-term memory lives in memory.md (prompt/section/memory injects it)
  - Background review is the main automatic path; use /memory policy approve to stage
  - Secrets-looking content is rejected
  - Dreaming and skills: /learn dream …, /learn skill …`
}
