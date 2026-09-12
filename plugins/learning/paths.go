package learning

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/plugins/learning/dreaming"
	rw "github.com/lengzhao/agentkit/runtime/workspace"
)

const (
	// DefaultMemoryRoot is the workspace-relative directory for memory.md (tenant local root).
	DefaultMemoryRoot     = "."
	DefaultMemoryFile     = "memory.md"
	DefaultCharLimit      = 2200
	DefaultDreamsFile     = "DREAMS.md"
	DefaultDreamingSubdir = "memory/dreaming"
	DefaultStagedSubdir   = "memory/.staged"
)

// joinUnderMemoryRoot joins segments under memoryRoot.
// memoryRoot may be "." (tenant local root), "global:.", "local:memory", etc.
func joinUnderMemoryRoot(root string, parts ...string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultMemoryRoot
	}
	tail := filepath.ToSlash(filepath.Join(parts...))
	if scope, base, scoped := rw.ParseScoped(root); scoped {
		if tail == "" || tail == "." {
			return scope + ":" + base
		}
		if base == "" || base == "." {
			return scope + ":" + tail
		}
		return scope + ":" + filepath.ToSlash(filepath.Join(base, tail))
	}
	if root == "." {
		if tail == "" || tail == "." {
			return "."
		}
		return tail
	}
	if tail == "" || tail == "." {
		return root
	}
	return filepath.ToSlash(filepath.Join(root, tail))
}

func stagedRelPath(root string) string {
	return joinUnderMemoryRoot(root, "memory", ".staged")
}

func reviewQuotaRelPath(root string) string {
	return joinUnderMemoryRoot(root, DefaultDreamingSubdir, "review_quota.json")
}

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
	return joinUnderMemoryRoot(root, file)
}

func dreamsRelPath(root string) string {
	return joinUnderMemoryRoot(root, DefaultDreamsFile)
}

func dreamingStateRelPath(root string) string {
	return joinUnderMemoryRoot(root, DefaultDreamingSubdir, "state.json")
}

func dreamingDeepRelPath(root string) string {
	return joinUnderMemoryRoot(root, DefaultDreamingSubdir, "deep")
}

// FormatHelp returns /learn usage text.
func FormatHelp() string {
	return `Usage:
  /learn                         show this help
  /learn show                    show memory.md (not staged pending)
  /learn memory <text>           append a memory entry immediately
  /learn remove <text>           remove entries containing <text>
  /learn session                 learn from current session user messages
  /learn dream status            show dreaming state
  /learn dream run               run Light → REM → Deep sweep now
  /learn dream on|off            enable or disable background dreaming
  /learn skill [focus]           create a skill workshop proposal from session
  /learn workshop list           list pending skill proposals
  /learn workshop show <id>      show one proposal
  /learn workshop apply <id>     apply a pending proposal to skills/
  /learn workshop reject <id>    reject a pending proposal
  /learn pending                 list staged memory and skill proposals
  /learn approve <id|all>        apply staged memory to memory.md
  /learn reject <id|all>         drop staged memory (not workshop)
  /learn policy                  show memory/skills write policy
  /learn policy memory approve|auto   background-review staging
  /learn policy skills off|propose|auto
  /learn policy reset            use config defaults again
  /learn help                    show this help

Notes:
  - Long-term memory lives in memory.md (prompt/section/memory injects it)
  - Dream Diary lives in DREAMS.md (human review only, not injected)
  - Skill proposals live under skills/.workshop/ until applied
  - Default review memory is auto; use /learn policy memory approve to stage until approve
  - Skills default propose; /learn policy skills auto|off adjusts tenant behavior
  - Secrets-looking content is rejected`
}

// FormatMemory renders memory entries for display.
func FormatMemory(entries []MemoryEntry, used, limit int) string {
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

func formatSweepResult(res *dreaming.SweepResult) string {
	if res == nil {
		return "dreaming sweep completed"
	}
	return fmt.Sprintf("dreaming sweep: sessions=%d signals=%d promoted=%d skipped=%d themes=%s",
		res.SessionsIngested, res.SignalsIngested, len(res.Promoted), res.Skipped, strings.Join(res.Themes, ", "))
}
