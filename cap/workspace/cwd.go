package workspace

import "context"

type workingDirectoryKey struct{}

// WithWorkingDirectory sets an execution directory without changing workspace
// roots or the host process cwd. An empty value leaves consumer defaults intact.
func WithWorkingDirectory(ctx context.Context, cwd string) context.Context {
	return context.WithValue(ctx, workingDirectoryKey{}, cwd)
}
func WorkingDirectory(ctx context.Context) string {
	cwd, _ := ctx.Value(workingDirectoryKey{}).(string)
	return cwd
}

// ResolveWorkingDirectory applies the turn's binding before a consumer's static
// default. Explicit request paths should be resolved separately by the consumer.
func ResolveWorkingDirectory(ctx context.Context, ws Service, fallback string) (string, error) {
	if cwd := WorkingDirectory(ctx); cwd != "" {
		return cwd, nil
	}
	return ws.Resolve(ctx, fallback)
}

// DefaultDirectoryResolver optionally selects an execution directory independently
// of storage roots. Empty means preserve legacy per-consumer defaults. Resolution
// is pure; the turn preparer creates a selected directory before execution.
type DefaultDirectoryResolver interface {
	DefaultWorkingDirectory(context.Context) (string, error)
}
