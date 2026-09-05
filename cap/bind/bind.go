package bind

import "context"

// Resolver resolves ctx:-prefixed bind sources from the current turn context.
type Resolver interface {
	ResolveCtxValue(ctx context.Context, from string) (string, error)
}
