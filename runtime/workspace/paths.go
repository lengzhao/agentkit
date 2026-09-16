package workspace

import (
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/runtime/workspace/workpath"
)

// defaultWorkDir and defaultUploadSubdir match L0 workspace.config defaults when fields are omitted.
const defaultWorkDir = "work"
const defaultUploadSubdir = "upload"

func normalizeWorkDir(workDir string) string {
	workDir = filepath.ToSlash(strings.TrimSpace(workDir))
	workDir = strings.TrimPrefix(workDir, "./")
	lower := strings.ToLower(workDir)
	if strings.HasPrefix(lower, "local:") {
		workDir = workDir[len("local:"):]
	}
	if workDir == "" {
		return defaultWorkDir
	}
	return workDir
}

func normalizeUploadSub(sub string) string {
	sub = filepath.ToSlash(strings.TrimSpace(sub))
	sub = strings.TrimPrefix(sub, "/")
	if sub == "" {
		return defaultUploadSubdir
	}
	return sub
}

func joinWorkUpload(workDir, uploadSub string) string {
	return workpath.JoinWork(workDir, uploadSub)
}
