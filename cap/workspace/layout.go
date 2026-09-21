package workspace

// Layout reports tenant-local agent directory layout (inbound uploads, shell cwd).
// workspace/default and workspace/tenant implement this; keep filesystem/local.config.root
// and tool/shell-bash.config.workDir aligned with WorkDirRel().
type Layout interface {
	Service
	WorkDirRel() string
	UploadDirRel() string
}
