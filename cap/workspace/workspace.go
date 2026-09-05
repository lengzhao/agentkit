package workspace

import "context"

const (
	ScopeGlobal = "global"
	ScopeLocal  = "local"
)

// Service resolves config-relative paths for the current request context.
// Implementations may scope roots by session, agent, or other ctx values;
// swap the workspace plugin to change isolation policy.
type Service interface {
	Resolve(ctx context.Context, rel string) (string, error)
}
