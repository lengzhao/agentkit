package media

import (
	"context"
	"path/filepath"
	"strings"

	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/workspace/workpath"
)

// CanonicalStoredPath normalizes attachment paths for session JSONL: work-relative, no scope prefixes.
// Model-facing text uses AgentLLMPath (absolute host paths).
func CanonicalStoredPath(ws cw.Service, rel string) string {
	rel = strings.TrimSpace(rel)
	if filepath.IsAbs(rel) {
		workDir, _ := workpath.WorkLayout(ws)
		if workDir == "" {
			return filepath.ToSlash(rel)
		}
		if ctx := context.Background(); ws != nil {
			workAbs, err := workpath.ResolveFile(ctx, ws, workDir)
			if err == nil {
				if r, err := filepath.Rel(workAbs, filepath.Clean(rel)); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
					return filepath.ToSlash(r)
				}
			}
		}
		return filepath.ToSlash(rel)
	}
	rel = stripScopedPrefixes(rel)
	workDir, _ := workpath.WorkLayout(ws)
	return filepath.ToSlash(workpath.StripWorkPrefix(workDir, rel))
}

// NormalizeWorkRel strips the configured work-dir prefix from a tenant-local relative path.
// Absolute paths are returned unchanged.
func NormalizeWorkRel(ws cw.Service, rel string) string {
	rel = strings.TrimSpace(rel)
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel)
	}
	workDir, _ := workpath.WorkLayout(ws)
	return workpath.StripWorkPrefix(workDir, stripScopedPrefixes(rel))
}

// CanonicalWorkPath returns the agent-facing absolute path.
func CanonicalWorkPath(ws cw.Service, rel string) string {
	return AgentLLMPath(context.Background(), ws, rel)
}

func stripLocalPrefix(path string) string {
	return stripScopedPrefixes(path)
}

func resolveFileAbs(ctx context.Context, ws cw.Service, path string) (string, error) {
	return workpath.ResolveFile(ctx, ws, path)
}
