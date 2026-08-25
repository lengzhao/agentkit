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
}

type Service struct {
	globalRoot string
	localRoot  string
	scope      string
}

func init() {
	pluginkit.Register("workspace/default", New)
}

// New registers workspace/default: Dual-root workspace: a global home and a local .agentkit directory under the project.
//
// Best practices:
//   - Prefix a path with global: or local: to pin it regardless of scope.
func New(cfg Config) (cw.Service, error) {
	global := cfg.Global
	if global == "" {
		global = cfg.Root
	}
	if global == "" {
		global = "~/.agentkit"
	}
	local := cfg.Local
	if local == "" {
		local = ".agentkit"
	}
	scope := cfg.Scope
	if scope == "" {
		scope = cw.ScopeGlobal
	}
	if scope != cw.ScopeGlobal && scope != cw.ScopeLocal {
		return nil, fmt.Errorf("workspace scope must be %q or %q", cw.ScopeGlobal, cw.ScopeLocal)
	}

	globalAbs, err := cw.Resolve(global)
	if err != nil {
		return nil, err
	}
	localAbs, err := cw.Resolve(local)
	if err != nil {
		return nil, err
	}
	return &Service{globalRoot: globalAbs, localRoot: localAbs, scope: scope}, nil
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
	return cw.ResolveRel(root, path)
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
