package media

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	cw "github.com/lengzhao/agentkit/cap/workspace"
	rtws "github.com/lengzhao/agentkit/runtime/workspace"
)

var scopedPathInText = regexp.MustCompile(`(?i)(?:local|global):[^\s\]\)\"'<>]+`)

// AgentLLMPath formats a path for model-facing text (tool results, prompts, history).
// local:/global: appear only in configuration and are never shown to the model.
// Resolved paths are always absolute host paths (bash, read/write, skill bundles).
func AgentLLMPath(ctx context.Context, ws cw.Service, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if isAbsPath(path) {
		return filepath.Clean(path)
	}
	if ws == nil {
		return filepath.Clean(stripScopedPrefixes(path))
	}
	abs, err := rtws.ResolveFile(ctx, ws, path)
	if err != nil {
		if abs, err = ws.Resolve(ctx, stripScopedPrefixes(path)); err != nil {
			return stripScopedPrefixes(path)
		}
	}
	return filepath.Clean(abs)
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
