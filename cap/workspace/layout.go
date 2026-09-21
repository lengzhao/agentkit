package workspace

// Layout reports tenant-local agent directory layout (inbound uploads, shell cwd).
// workspace/default and workspace/tenant implement this; keep filesystem/local.config.root
// and tool/shell-bash.config.workDir aligned with WorkDirRel().
type Layout interface {
	Service
	WorkDirRel() string
	UploadDirRel() string
}

// WorkLayout reads agent work layout from the workspace service.
func WorkLayout(ws Service) (workDir, uploadDir string) {
	if ws == nil {
		return "", ""
	}
	if l, ok := ws.(Layout); ok {
		return l.WorkDirRel(), l.UploadDirRel()
	}
	return "", ""
}
