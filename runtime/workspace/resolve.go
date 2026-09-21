package workspace

import (
	"context"
	"path/filepath"
	"strings"

	cw "github.com/lengzhao/agentkit/cap/workspace"
)

// ResolveFile maps a stored or model path to an absolute filesystem path.
// Accepts local:/global: scoped paths or tenant-root-relative paths (work/upload/…).
func ResolveFile(ctx context.Context, ws cw.Service, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ErrEmptyPath
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, cw.ScopeLocal+":") || strings.HasPrefix(lower, cw.ScopeGlobal+":") {
		return ws.Resolve(ctx, path)
	}
	workDir, _ := cw.WorkLayout(ws)
	if workDir == "" {
		return ws.Resolve(ctx, path)
	}
	return ws.Resolve(ctx, cw.JoinWork(workDir, cw.StripWorkPrefix(workDir, path)))
}

// ErrEmptyPath is returned when ResolveFile gets an empty path.
var ErrEmptyPath = errEmptyPath{}

type errEmptyPath struct{}

func (errEmptyPath) Error() string { return "empty path" }
