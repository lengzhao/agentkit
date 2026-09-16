package media

import (
	"context"

	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/workspace/workpath"
)

// CanonicalStoredPath normalizes attachment paths for session JSONL (no local:/global: prefixes).
func CanonicalStoredPath(ws cw.Service, rel string) string {
	return AgentLLMPath(context.Background(), ws, rel)
}

// NormalizeWorkRel strips the configured work-dir prefix from a tenant-local relative path.
func NormalizeWorkRel(ws cw.Service, rel string) string {
	workDir, _ := workpath.WorkLayout(ws)
	return workpath.StripWorkPrefix(workDir, stripScopedPrefixes(rel))
}

// CanonicalWorkPath returns the agent-facing path under the configured work layout.
func CanonicalWorkPath(ws cw.Service, rel string) string {
	return CanonicalStoredPath(ws, rel)
}

func stripLocalPrefix(path string) string {
	return stripScopedPrefixes(path)
}

func resolveFileAbs(ctx context.Context, ws cw.Service, path string) (string, error) {
	return workpath.ResolveFile(ctx, ws, path)
}
