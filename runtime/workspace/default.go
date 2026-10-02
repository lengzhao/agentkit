package workspace

import (
	"context"
	"fmt"

	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	// Root is single-root shorthand, kept for older configs; prefer Global and Local.
	Root string `json:"root"` // deprecated: alias for global
	// Global is global root, conventionally ~/.agentkit.
	Global string `json:"global"`
	// Local is local root, default .agentkit under cwd.
	Local string `json:"local"`
	// Scope is which root an unprefixed path resolves against: global or local.
	Scope string `json:"scope"` // global | local
	// WorkDir is the tenant-local agent work subtree (align with filesystem/local root and shell workDir).
	WorkDir string `json:"workDir,omitempty"`
	// UploadSubdir is the inbound upload folder under WorkDir (default upload).
	UploadSubdir string `json:"uploadSubdir,omitempty"`
}

type Service struct {
	globalRoot  string
	localRoot   string
	scope       string
	workDir     string
	uploadSub   string
}

func init() {
	pluginkit.Register("workspace/default", New)
}

// New registers workspace/default: Dual-root workspace: a global home and a local .agentkit directory under the project.
//
// Best practices:
//   - Prefix a path with global: or local: to pin it regardless of scope.
// SetDefaults implements pluginkit.Defaulter.
func (c *Config) SetDefaults() {
	if c.Global == "" {
		c.Global = c.Root // deprecated alias
	}
	if c.Global == "" {
		c.Global = "~/.agentkit"
	}
	if c.Local == "" {
		c.Local = ".agentkit"
	}
	if c.Scope == "" {
		c.Scope = cw.ScopeGlobal
	}
	if c.WorkDir == "" {
		c.WorkDir = defaultWorkDir
	}
	if c.UploadSubdir == "" {
		c.UploadSubdir = defaultUploadSubdir
	}
}

// Validate implements pluginkit.Validator.
func (c *Config) Validate() error {
	if c.Scope != cw.ScopeGlobal && c.Scope != cw.ScopeLocal {
		return fmt.Errorf("workspace scope must be %q or %q", cw.ScopeGlobal, cw.ScopeLocal)
	}
	return nil
}

func New(cfg Config) (cw.Service, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	globalAbs, err := Resolve(cfg.Global)
	if err != nil {
		return nil, err
	}
	localAbs, err := Resolve(cfg.Local)
	if err != nil {
		return nil, err
	}
	workDir := normalizeWorkDir(cfg.WorkDir)
	uploadSub := normalizeUploadSub(cfg.UploadSubdir)
	return &Service{
		globalRoot: globalAbs,
		localRoot:  localAbs,
		scope:      cfg.Scope,
		workDir:    workDir,
		uploadSub:  uploadSub,
	}, nil
}

func (s *Service) WorkDirRel() string {
	return s.workDir
}

func (s *Service) UploadDirRel() string {
	return joinWorkUpload(s.workDir, s.uploadSub)
}

func (s *Service) Resolve(_ context.Context, rel string) (string, error) {
	scope, path, scoped := cw.ParseScoped(rel)
	if !scoped {
		scope = s.scope
		path = rel
	}
	root, err := s.rootFor(scope)
	if err != nil {
		return "", err
	}
	return ResolveRel(root, path)
}

func (s *Service) rootFor(scope string) (string, error) {
	switch scope {
	case cw.ScopeGlobal:
		return s.globalRoot, nil
	case cw.ScopeLocal:
		return s.localRoot, nil
	default:
		return "", fmt.Errorf("unknown workspace scope %q", scope)
	}
}

var _ cw.Service = (*Service)(nil)
var _ cw.Layout = (*Service)(nil)
