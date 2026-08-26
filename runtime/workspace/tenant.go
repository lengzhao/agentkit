package workspace

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/lengzhao/agentkit/cap/tenant"
	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/pluginkit"
)

// TenantConfig configures workspace/tenant.
type TenantConfig struct {
	// Global is the root shared by every tenant, reached with the global: prefix.
	// Conventionally ~/.agentkit. Shared skills, agent definitions and mcp.json
	// live here; nothing under it is tenant-scoped.
	Global string `json:"global"`
	// LocalBase is the parent directory of per-tenant roots. A tenant with no
	// entry in Tenants gets LocalBase/<sanitized tenant key>, so adding a Slack
	// channel needs no configuration at all: it is isolated by default.
	LocalBase string `json:"localBase"`
	// Scope is which root an unprefixed path resolves against: global or local.
	// Defaults to local, because the point of this plugin is that unqualified
	// paths land in the caller's own tenant.
	Scope string `json:"scope"`
	// Tenants pins an explicit root for specific tenant keys, e.g.
	// "slack:C123ABC": {root: ~/work/project-a}. Keys are tenant keys as derived
	// by cap/tenant.Key, not session ids: every thread and every user in a Slack
	// channel shares the channel's entry.
	Tenants map[string]TenantEntry `json:"tenants,omitempty"`
}

// TenantEntry is one tenant's pinned workspace root.
type TenantEntry struct {
	// Root is an absolute path or ~/ path used verbatim as this tenant's local root.
	Root string `json:"root"`
}

// DefaultTenantDir is the local root used when a request carries no session id,
// and therefore no tenant. Timers, cron tasks and direct library calls land
// here rather than in some other tenant's directory.
const DefaultTenantDir = "_default"

// TenantService resolves paths against a shared global root and a per-tenant
// local root.
type TenantService struct {
	globalRoot string
	localBase  string
	scope      string
	roots      map[string]string
}

func init() {
	pluginkit.Register("workspace/tenant", NewTenant)
}

// NewTenant registers workspace/tenant: Per-tenant local root with one shared global root, keyed by the tenant behind the current session.
//
// Every path in the system is resolved through workspace, so scoping the local
// root per tenant is what actually separates two Slack channels: sessions, the
// filesystem tool, shell, skills and AGENTS.md all follow without changes.
//
// Best practices:
//   - Leave a tenant out of tenants to get LocalBase/<key>; list it only to pin
//     an existing project directory.
//   - Keep genuinely shared assets under global: — skills, agent definitions,
//     mcp.json — and everything the agent may write under an unprefixed path.
//   - Raise runner.maxConcurrentTurns once roots are separated: the reason it
//     defaults to 1 is that turns share a workdir, which stops being true here.
//   - Unlike workspace/default, ".." never leaves a root. A per-tenant root sits
//     beside its siblings, so the parent-directory exemption would be a way out
//     of the tenant. Point a tenant's root at the project directory itself
//     instead of relying on "..".
func NewTenant(cfg TenantConfig) (cw.Service, error) {
	global := cfg.Global
	if global == "" {
		global = "~/.agentkit"
	}
	localBase := cfg.LocalBase
	if localBase == "" {
		localBase = "~/.agentkit/tenants"
	}
	scope := cfg.Scope
	if scope == "" {
		scope = cw.ScopeLocal
	}
	if scope != cw.ScopeGlobal && scope != cw.ScopeLocal {
		return nil, fmt.Errorf("workspace scope must be %q or %q", cw.ScopeGlobal, cw.ScopeLocal)
	}

	globalAbs, err := cw.Resolve(global)
	if err != nil {
		return nil, err
	}
	localBaseAbs, err := cw.Resolve(localBase)
	if err != nil {
		return nil, err
	}

	roots := make(map[string]string, len(cfg.Tenants))
	for key, entry := range cfg.Tenants {
		if key == "" {
			return nil, fmt.Errorf("workspace/tenant: empty tenant key")
		}
		if entry.Root == "" {
			return nil, fmt.Errorf("workspace/tenant: tenant %q has no root", key)
		}
		root, err := cw.Resolve(entry.Root)
		if err != nil {
			return nil, fmt.Errorf("workspace/tenant: tenant %q root: %w", key, err)
		}
		roots[key] = root
	}

	return &TenantService{
		globalRoot: globalAbs,
		localBase:  localBaseAbs,
		scope:      scope,
		roots:      roots,
	}, nil
}

func (s *TenantService) Resolve(ctx context.Context, rel string) (string, error) {
	scope, path, scoped := cw.ParseScoped(rel)
	if !scoped {
		scope = s.scope
		path = rel
	}
	root, err := s.rootFor(ctx, scope)
	if err != nil {
		return "", err
	}
	return cw.ResolveRelStrict(root, path)
}

func (s *TenantService) rootFor(ctx context.Context, scope string) (string, error) {
	switch scope {
	case cw.ScopeGlobal:
		return s.globalRoot, nil
	case cw.ScopeLocal:
		return s.TenantRoot(ctx), nil
	default:
		return "", fmt.Errorf("unknown workspace scope %q", scope)
	}
}

// TenantRoot reports the local root the current context resolves against.
func (s *TenantService) TenantRoot(ctx context.Context) string {
	key := tenant.FromContext(ctx)
	if root, ok := s.roots[key]; ok {
		return root
	}
	dir := tenant.DirName(key)
	if dir == "" {
		dir = DefaultTenantDir
	}
	return filepath.Join(s.localBase, dir)
}

var _ cw.Service = (*TenantService)(nil)
