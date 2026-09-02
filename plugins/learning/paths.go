package learning

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/plugins/learning/dreaming"
	"github.com/lengzhao/agentkit/plugins/learning/workshop"
)

const (
	// DefaultMemoryRoot is the workspace-relative directory for memory.md (tenant local root).
	DefaultMemoryRoot     = "."
	DefaultMemoryFile     = "memory.md"
	DefaultCharLimit      = 2200
	DefaultDreamsFile     = "DREAMS.md"
	DefaultDreamingSubdir = "memory/dreaming"
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

func dreamsRelPath(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultMemoryRoot
	}
	if root == "." {
		return DefaultDreamsFile
	}
	return root + "/" + DefaultDreamsFile
}

func dreamingStateRelPath(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultMemoryRoot
	}
	return filepath.ToSlash(filepath.Join(root, DefaultDreamingSubdir, "state.json"))
}

func dreamingDeepRelPath(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultMemoryRoot
	}
	return filepath.ToSlash(filepath.Join(root, DefaultDreamingSubdir, "deep"))
}

func workshopRelPath(skillsDir string) string {
	skillsDir = strings.TrimSpace(skillsDir)
	if skillsDir == "" {
		skillsDir = workshop.Defaults().SkillsDir
	}
	return skillsDir + "/.workshop"
}

// FormatHelp returns /learn usage text.
func FormatHelp() string {
	return `Usage:
  /learn                         show personal memory
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
  /learn help                    show this help

Notes:
  - Long-term memory lives in memory.md (prompt/section/memory injects it)
  - Dream Diary lives in DREAMS.md (human review only, not injected)
  - Skill proposals live under skills/.workshop/ until applied
  - Secrets-looking content is rejected`
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

func formatSweepResult(res *dreaming.SweepResult) string {
	if res == nil {
		return "dreaming sweep completed"
	}
	return fmt.Sprintf("dreaming sweep: sessions=%d signals=%d promoted=%d skipped=%d themes=%s",
		res.SessionsIngested, res.SignalsIngested, len(res.Promoted), res.Skipped, strings.Join(res.Themes, ", "))
}
