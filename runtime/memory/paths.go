package memory

import (
	"path/filepath"
	"strings"

	cw "github.com/lengzhao/agentkit/cap/workspace"
)

const (
	DefaultRoot = "."
	DefaultFile = "memory.md"
)

// JoinUnderRoot joins segments under memoryRoot (".", "global:.", "local:memory", …).
func JoinUnderRoot(root string, parts ...string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultRoot
	}
	tail := filepath.ToSlash(filepath.Join(parts...))
	if scope, base, scoped := cw.ParseScoped(root); scoped {
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

func MemoryFileRel(root, file string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultRoot
	}
	file = strings.TrimSpace(file)
	if file == "" {
		file = DefaultFile
	}
	return JoinUnderRoot(root, file)
}

func StagedDirRel(root string) string {
	return JoinUnderRoot(root, "memory", ".staged")
}

func LedgerRel(root string) string {
	return JoinUnderRoot(root, "memory", "ledger.jsonl")
}

func MemoryPolicyRel(root string) string {
	return JoinUnderRoot(root, "memory", "policy.json")
}

func SkillsPolicyRel(root string) string {
	return JoinUnderRoot(root, "learning", "policy.json")
}
