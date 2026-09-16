package media

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/workspace/workpath"
)

var scopedPathInText = regexp.MustCompile(`(?i)(?:local|global):[^\s\]\)\"'<>]+`)

// AgentLLMPath formats a path for model-facing text and session storage.
// local:/global: appear only in configuration. Paths under the agent work directory
// are relative to that directory (e.g. upload/foo, same as shell cwd and fs root);
// global and out-of-work files use absolute paths.
func AgentLLMPath(ctx context.Context, ws cw.Service, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if isAbsPath(path) {
		return filepath.Clean(path)
	}
	if ws == nil {
		return stripWorkPrefixFromPath(stripScopedPrefixes(path))
	}
	abs, err := workpath.ResolveFile(ctx, ws, path)
	if err != nil {
		inner := stripScopedPrefixes(path)
		workDir, _ := workpath.WorkLayout(ws)
		if workDir != "" {
			canon := workpath.CanonicalWorkPath(workDir, inner)
			return workpath.StripWorkPrefix(workDir, canon)
		}
		return workpath.StripWorkPrefix("work", stripWorkPrefixFromPath(inner))
	}
	return agentPathFromAbs(ctx, ws, abs)
}

// AgentDisplayPath is AgentLLMPath without request context; prefer AgentLLMPath when ctx carries tenant workspace.
func AgentDisplayPath(ws cw.Service, path string) string {
	return AgentLLMPath(context.Background(), ws, path)
}

// RewritePathsInText replaces scoped workspace path tokens in free text for LLM history.
func RewritePathsInText(ctx context.Context, ws cw.Service, text string) string {
	if ws == nil || text == "" {
		return text
	}
	return scopedPathInText.ReplaceAllStringFunc(text, func(match string) string {
		return AgentLLMPath(ctx, ws, match)
	})
}

func agentPathFromAbs(ctx context.Context, ws cw.Service, abs string) string {
	abs = filepath.Clean(abs)
	workDir, _ := workpath.WorkLayout(ws)
	if workDir == "" {
		return abs
	}
	workAbs, err := workpath.ResolveFile(ctx, ws, workDir)
	if err != nil {
		return abs
	}
	rel, err := filepath.Rel(workAbs, abs)
	if err != nil {
		return abs
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs
	}
	if rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

// stripWorkPrefixFromPath drops a leading work/ segment when workspace layout is unknown.
func stripWorkPrefixFromPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if strings.HasPrefix(path, "work/") {
		return path[len("work/"):]
	}
	if path == "work" {
		return "."
	}
	return path
}

func isAbsPath(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}
	return len(path) >= 2 && path[1] == ':'
}

func stripScopedPrefixes(path string) string {
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
