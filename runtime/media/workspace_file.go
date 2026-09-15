package media

import (
	"context"
	"strings"

	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/workspace/workpath"
)

// CanonicalStoredPath normalizes attachment/read paths for session storage and prompts (local:…).
func CanonicalStoredPath(ws cw.Service, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	lower := strings.ToLower(rel)
	if strings.HasPrefix(lower, cw.ScopeLocal+":") || strings.HasPrefix(lower, cw.ScopeGlobal+":") {
		return rel
	}
	workDir, _ := workpath.WorkLayout(ws)
	if workDir == "" {
		return workpath.LocalPath(rel)
	}
	return workpath.LocalPath(workpath.CanonicalWorkPath(workDir, rel))
}

// NormalizeWorkRel strips the configured work-dir prefix from a tenant-local relative path.
func NormalizeWorkRel(ws cw.Service, rel string) string {
	workDir, _ := workpath.WorkLayout(ws)
	return workpath.StripWorkPrefix(workDir, stripLocalPrefix(rel))
}

// CanonicalWorkPath returns local:-scoped path under the configured work dir.
func CanonicalWorkPath(ws cw.Service, rel string) string {
	return CanonicalStoredPath(ws, rel)
}

func stripLocalPrefix(path string) string {
	path = strings.TrimSpace(path)
	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, cw.ScopeLocal+":") {
		return path[len(cw.ScopeLocal)+1:]
	}
	if strings.HasPrefix(lower, cw.ScopeGlobal+":") {
		return path[len(cw.ScopeGlobal)+1:]
	}
	return path
}

func resolveFileAbs(ctx context.Context, ws cw.Service, path string) (string, error) {
	return workpath.ResolveFile(ctx, ws, path)
}
