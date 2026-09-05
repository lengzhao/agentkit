package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cw "github.com/lengzhao/agentkit/cap/workspace"
)

// ParseScoped splits scoped paths such as "global:skills" or "local:.".
// Bare paths and absolute paths (~/foo, /abs) are not scoped.
func ParseScoped(rel string) (scope, path string, ok bool) {
	i := strings.Index(rel, ":")
	if i <= 0 {
		return "", rel, false
	}
	prefix := rel[:i]
	if prefix != cw.ScopeGlobal && prefix != cw.ScopeLocal {
		return "", rel, false
	}
	path = rel[i+1:]
	if path == "" {
		path = "."
	}
	return prefix, path, true
}

// Resolve expands ~/ and returns an absolute path. Use at build time for workspace.root config.
func Resolve(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return filepath.Abs(path)
}

// ResolveRel maps rel against base. Absolute paths and ~/ bypass base.
// Resolving one level above base is allowed: when the local root is
// <project>/.agentkit, tools reach the project itself through "..".
func ResolveRel(base, rel string) (string, error) {
	return resolveRel(base, rel, true)
}

// ResolveRelStrict is ResolveRel with the parent-directory exemption removed:
// nothing outside base resolves, not even one level up. Use it for roots that
// are an isolation boundary rather than a convenience default — a per-tenant
// root sits next to its siblings, so allowing ".." there would let one tenant
// read and write another's workdir.
func ResolveRelStrict(base, rel string) (string, error) {
	return resolveRel(base, rel, false)
}

func resolveRel(base, rel string, allowParent bool) (string, error) {
	if rel == "" {
		rel = "."
	}
	if rel == "~" || strings.HasPrefix(rel, "~/") {
		return Resolve(rel)
	}
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel), nil
	}
	base = filepath.Clean(base)
	if rel == "." {
		return base, nil
	}
	full := filepath.Join(base, rel)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	relToBase, err := filepath.Rel(base, abs)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(relToBase, "..") {
		return abs, nil
	}
	if !allowParent {
		return "", fmt.Errorf("path escapes work dir: %s", rel)
	}
	parent, err := filepath.Abs(filepath.Join(base, ".."))
	if err != nil {
		return "", err
	}
	relToParent, err := filepath.Rel(parent, abs)
	if err != nil || strings.HasPrefix(relToParent, "..") {
		return "", fmt.Errorf("path escapes work dir: %s", rel)
	}
	return abs, nil
}

// Static returns a fixed-root Service for tests.
func Static(root string) cw.Service {
	return &staticService{root: filepath.Clean(root)}
}

type staticService struct {
	root string
}

func (s *staticService) Resolve(_ context.Context, rel string) (string, error) {
	return ResolveRel(s.root, rel)
}
