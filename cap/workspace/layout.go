package workspace

// Layout reports tenant-local agent directory layout (inbound uploads, shell cwd).
// workspace/default and workspace/tenant implement this; keep tool/fs-workspace.config.root
// and tool/shell-bash.config.workDir aligned with WorkDirRel().
type Layout interface {
	Service
	WorkDirRel() string
	UploadDirRel() string
}
