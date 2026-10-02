// Package sandbox defines the tenant sandbox capability: one view config, two
// enforcement mechanisms. Standard kind: sandbox/bwrap (runtime/sandbox).
//
// The view is rendered per call from the tenant identity in ctx: tenant root
// (rw), global root (ro, secret files masked), neighbor tenants masked,
// minimal system binds. Consumers:
//
//   - Subprocess tools/agents (tool/shell-bwrap, agent/acp-remote) wrap their
//     exec argv with WrapArgv (bwrap mount-namespace isolation);
//   - In-process tools (filesystem/sandbox decorator → tool/fs-workspace) call
//     CheckRead/CheckWrite, since bwrap cannot confine Go code in the runner.
//
// Note: in-process checks are best-effort — a symlink created between the
// check and the file operation (TOCTOU) can escape them. Strong isolation
// comes from the subprocess path (WrapArgv); CheckRead/CheckWrite exist to
// keep the common model-tool flow aligned with the same view, not to resist
// a deliberately malicious in-process actor.
package sandbox

import (
	"context"
	"errors"
)

// ErrDenied is returned when a path is denied by the sandbox view (in-process
// enforcement). Callers may errors.Is(err, ErrDenied).
var ErrDenied = errors.New("sandbox: path denied")

// Service is the tenant sandbox view. Implementations must be safe for
// concurrent use and must resolve tenant identity from ctx on every call.
type Service interface {
	// Enabled reports whether sandboxing is active. When false, WrapArgv
	// returns inner unchanged and CheckRead/CheckWrite allow everything
	// (confinement falls back to the tool layer).
	Enabled() bool
	// WrapArgv wraps a subprocess command (e.g. ["bash","-lc",cmd] or an ACP
	// agent command) with the sandbox view for the current tenant. workDir is
	// the subprocess working directory (host absolute path).
	WrapArgv(ctx context.Context, workDir string, inner []string) ([]string, error)
	// CheckRead allows/denies in-process reads of a host absolute path. The
	// model mirrors the subprocess view's allowlist: only the tenant root,
	// global root and ro/rwBinds are visible; masked paths (hidePaths,
	// secretFiles) and everything else on the host are denied.
	CheckRead(ctx context.Context, path string) error
	// CheckWrite allows/denies in-process writes of a host absolute path:
	// only the tenant root and rwBinds are writable; everything else is denied
	// (the in-process equivalent of the sandbox's read-only floor, EROFS).
	CheckWrite(ctx context.Context, path string) error
}

// Disabled returns a no-op Service: Enabled reports false, WrapArgv passes
// inner through unchanged, CheckRead/CheckWrite allow everything
// (confinement falls back to the tool layer). Assembly code can inject it
// when no sandbox instance is configured, so consumers never nil-check.
func Disabled() Service { return disabled{} }

type disabled struct{}

func (disabled) Enabled() bool { return false }
func (disabled) WrapArgv(_ context.Context, _ string, inner []string) ([]string, error) {
	return inner, nil
}
func (disabled) CheckRead(context.Context, string) error  { return nil }
func (disabled) CheckWrite(context.Context, string) error { return nil }
