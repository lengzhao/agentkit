package workpath

import (
	"context"
	"path/filepath"
	"strings"

	cw "github.com/lengzhao/agentkit/cap/workspace"
)

// WorkLayout reads agent work layout from the workspace service.
func WorkLayout(ws cw.Service) (workDir, uploadDir string) {
	if ws == nil {
		return "", ""
	}
	if l, ok := ws.(cw.Layout); ok {
		return l.WorkDirRel(), l.UploadDirRel()
	}
	return "", ""
}

// AttachRel returns the tenant-root-relative path for a file under the configured upload directory.
func AttachRel(ws cw.Service, filename string) string {
	_, upload := WorkLayout(ws)
	return filepath.ToSlash(filepath.Join(upload, filename))
}

// StripWorkPrefix removes leading workDir/ segments from tenant-root-relative paths.
func StripWorkPrefix(workDir, rel string) string {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.TrimPrefix(rel, "/")
	workDir = filepath.ToSlash(strings.TrimSpace(workDir))
	workDir = strings.TrimPrefix(workDir, "./")
	if workDir == "" {
		return rel
	}
	if rel == workDir {
		return ""
	}
	prefix := workDir + "/"
	for strings.HasPrefix(rel, prefix) {
		rel = strings.TrimPrefix(rel, prefix)
	}
	return rel
}

// JoinWork builds a tenant-root-relative path under workDir.
func JoinWork(workDir, rel string) string {
	workDir = filepath.ToSlash(strings.TrimSpace(workDir))
	workDir = strings.TrimPrefix(workDir, "./")
	if workDir == "" {
		return filepath.ToSlash(strings.TrimSpace(rel))
	}
	inner := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(rel)), "/")
	if inner == "" {
		return workDir
	}
	inner = StripWorkPrefix(workDir, inner)
	if inner == "" {
		return workDir
	}
	return filepath.ToSlash(filepath.Join(workDir, inner))
}

// CanonicalWorkPath normalizes to tenant-root-relative form under workDir.
// Scoped paths (global:/local:) and Windows drive paths are returned unchanged.
func CanonicalWorkPath(workDir, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	lower := strings.ToLower(rel)
	if strings.HasPrefix(lower, cw.ScopeGlobal+":") || strings.HasPrefix(lower, cw.ScopeLocal+":") {
		return filepath.ToSlash(rel)
	}
	if filepath.IsAbs(rel) {
		if len(rel) >= 2 && rel[1] == ':' {
			return filepath.ToSlash(rel)
		}
	}
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	if rel == "" {
		return ""
	}
	return JoinWork(workDir, rel)
}

// TrimRedundantFSRootPrefix drops a leading fsRoot/ prefix when the fs tool root is already fsRoot.
func TrimRedundantFSRootPrefix(fsRoot, path string) string {
	fsRoot = filepath.ToSlash(strings.TrimSpace(fsRoot))
	fsRoot = strings.TrimPrefix(fsRoot, "./")
	path = filepath.ToSlash(path)
	if fsRoot == "" || fsRoot == "." {
		return path
	}
	for {
		switch {
		case strings.HasPrefix(path, "./"+fsRoot+"/"):
			path = strings.TrimPrefix(path, "./"+fsRoot+"/")
		case strings.HasPrefix(path, fsRoot+"/"):
			path = strings.TrimPrefix(path, fsRoot+"/")
		default:
			return path
		}
	}
}

// ScopedPath prefixes a tenant-local relative path for workspace.Resolve.
func ScopedPath(scope, rel string) string {
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	if rel == "" {
		rel = "."
	}
	return scope + ":" + rel
}

// LocalPath prefixes a path under the tenant local root (e.g. local:work/upload/foo.jpg).
func LocalPath(rel string) string {
	return ScopedPath(cw.ScopeLocal, rel)
}

// ResolveFile maps a stored or model path to an absolute filesystem path.
// Accepts local:/global: scoped paths or tenant-root-relative paths (work/upload/…).
func ResolveFile(ctx context.Context, ws cw.Service, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ErrEmptyPath
	}
	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, cw.ScopeLocal+":") || strings.HasPrefix(lower, cw.ScopeGlobal+":") {
		return ws.Resolve(ctx, path)
	}
	workDir, _ := WorkLayout(ws)
	if workDir == "" {
		return ws.Resolve(ctx, path)
	}
	return ws.Resolve(ctx, JoinWork(workDir, StripWorkPrefix(workDir, path)))
}

// ErrEmptyPath is returned when ResolveFile gets an empty path.
var ErrEmptyPath = errEmptyPath{}

type errEmptyPath struct{}

func (errEmptyPath) Error() string { return "empty path" }
