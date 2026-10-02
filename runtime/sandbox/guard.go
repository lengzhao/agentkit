package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
)

// CheckRead decides per the current tenant view whether an in-process read is
// allowed (used by the filesystem/sandbox decorator and other in-process
// tools — bwrap cannot confine Go code). path is a host absolute path. When
// the sandbox is disabled everything is allowed (confinement falls back to
// the tool layer, e.g. filesystem/local root).
//
// The read model mirrors the bwrap view's allowlist: inside the sandbox only
// {tenantRoot, globalRoot, roBinds, rwBinds, systemBinds} exist and
// everything else is invisible; accordingly an in-process read is allowed
// only under {tenantRoot, globalRoot, roBinds, rwBinds} (system binds are a
// subprocess mechanism detail — fs tools have no legitimate need to read
// /usr; add an explicit roBind if one ever does). Deny rules:
//   - masked paths: matching hidePaths (incl. subpaths) or secretFiles;
//   - everything outside the allowlist (incl. neighbor tenants and any
//     unrelated host path).
func (s *Sandbox) CheckRead(ctx context.Context, path string) error {
	if !s.on {
		return nil
	}
	v, err := s.render(ctx)
	if err != nil {
		return err
	}
	return checkRead(v, cleanHostPath(path), path)
}

func checkRead(v *view, p, orig string) error {
	// Masked paths and secret files are invisible (checked first: a hidePath
	// may live inside an allowed root such as the global root).
	for _, h := range v.hidePaths {
		if within(p, h) {
			return fmt.Errorf("%w: read %q is masked", capsandbox.ErrDenied, orig)
		}
	}
	for _, f := range v.secretPaths {
		if p == f {
			return fmt.Errorf("%w: read %q is masked", capsandbox.ErrDenied, orig)
		}
	}
	// Allowlist, aligned with the bwrap view: only these roots are visible.
	if within(p, v.tenantRoot) || within(p, v.globalRoot) {
		return nil
	}
	for _, b := range v.roBinds {
		if within(p, b) {
			return nil
		}
	}
	for _, b := range v.rwBinds {
		if within(p, b) {
			return nil
		}
	}
	return fmt.Errorf("%w: read %q outside the sandbox view (tenant root, global root, ro/rwBinds)", capsandbox.ErrDenied, orig)
}

// CheckWrite decides per the current tenant view whether an in-process write
// is allowed. path is a host absolute path. Aligned with the bwrap view's
// read-only floor: only the tenant root and rwBinds are writable; everything
// else (global root, roBinds, system paths, unbound paths) is denied — the
// in-process equivalent of EROFS inside the sandbox.
func (s *Sandbox) CheckWrite(ctx context.Context, path string) error {
	if !s.on {
		return nil
	}
	v, err := s.render(ctx)
	if err != nil {
		return err
	}
	p := cleanHostPath(path)
	if err := checkRead(v, p, path); err != nil {
		return err
	}
	if within(p, v.tenantRoot) {
		return nil
	}
	for _, rw := range v.rwBinds {
		if within(p, rw) {
			return nil
		}
	}
	return fmt.Errorf("%w: write %q outside tenant root and rwBinds (read-only)", capsandbox.ErrDenied, path)
}

// cleanHostPath normalizes a host path; symlinks are resolved when the target
// exists, matching the realpath normalization of view rendering.
func cleanHostPath(path string) string {
	p := filepath.Clean(strings.TrimSpace(path))
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	// Target does not exist (e.g. a file about to be created): resolve its
	// parent so a symlinked parent cannot bypass the checks.
	dir := filepath.Dir(p)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(real, filepath.Base(p))
	}
	return p
}

// within reports whether p equals root or lives under it.
func within(p, root string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(os.PathSeparator))
}
