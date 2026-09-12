package session

import "strings"

// WorkspaceKeyFromLocalDir maps a tenant directory name under localBase back to a workspace key.
// This mirrors WorkspaceLocalDirName for the common platform_channel layout.
func WorkspaceKeyFromLocalDir(dirName string, omitPlatform bool) string {
	dirName = strings.TrimSpace(dirName)
	if dirName == "" || dirName == "_" {
		return ""
	}
	if omitPlatform {
		return dirName
	}
	if i := strings.Index(dirName, "_"); i > 0 {
		return dirName[:i] + ":" + dirName[i+1:]
	}
	return dirName
}
