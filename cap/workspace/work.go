package workspace

import (
	"path/filepath"
	"strings"
)

// AttachRel returns the tenant-root-relative path for a file under the configured upload directory.
func AttachRel(ws Service, filename string) string {
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

// NormalizeAgentRel maps agent-facing paths to tenant-root-relative form under workDir,
// stripping redundant work/ prefixes. Scoped and Windows drive paths pass through.
func NormalizeAgentRel(workDir, rel string) string {
	return StripWorkPrefix(workDir, CanonicalWorkPath(workDir, rel))
}

// UploadWorkRel is the upload directory relative to the agent work tree (e.g. upload).
func UploadWorkRel(ws Service) string {
	workDir, upload := WorkLayout(ws)
	return StripWorkPrefix(workDir, upload)
}

// AttachFSRel is the agent-facing path for a file under upload (e.g. upload/foo).
func AttachFSRel(ws Service, name string) string {
	workDir, _ := WorkLayout(ws)
	return StripWorkPrefix(workDir, AttachRel(ws, name))
}

// CanonicalWorkPath normalizes to tenant-root-relative form under workDir.
// Scoped paths (global:/local:) and Windows drive paths are returned unchanged.
func CanonicalWorkPath(workDir, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	lower := strings.ToLower(rel)
	if strings.HasPrefix(lower, ScopeGlobal+":") || strings.HasPrefix(lower, ScopeLocal+":") {
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

// ScopedPath prefixes a tenant-local relative path for workspace.Resolve.
func ScopedPath(scope, rel string) string {
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	if rel == "" {
		rel = "."
	}
	return scope + ":" + rel
}

// LocalPath adds the local: scope prefix for configuration (e.g. shell cwd, bootstrap workDir).
// Runtime code should use tenant-root-relative work/… paths and runtime/workspace.ResolveFile.
func LocalPath(rel string) string {
	return ScopedPath(ScopeLocal, rel)
}
