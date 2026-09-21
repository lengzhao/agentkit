package workspace

import "context"

const (
	ScopeGlobal = "global"
	ScopeLocal  = "local"
)

// Service maps configuration-relative paths to host absolute paths for the
// current request context. Resolve always returns a cleaned absolute path when
// it succeeds.
//
// The global:/local: prefixes (see ParseScoped) are configuration vocabulary
// only — YAML, preset fields, and plugin constructor config. After resolution
// (plugin init or per-request via Resolve / runtime/workspace.ResolveFile), runtime code
// and model-facing surfaces must not carry those prefixes.
type Service interface {
	Resolve(ctx context.Context, rel string) (string, error)
}
