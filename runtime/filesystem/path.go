package filesystem

import (
	"path/filepath"
	"strings"
)

// TrimRedundantFSRootPrefix drops a leading fsRoot/ prefix when the fs store root is already fsRoot.
// Used by filesystem/local when models pass paths like work/upload/foo while config.root is work.
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
