package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Service resolves config-relative paths for the current request context.
// Implementations may scope roots by session, agent, or other ctx values;
// swap the workspace plugin to change isolation policy.
type Service interface {
	Resolve(ctx context.Context, rel string) (string, error)
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
func ResolveRel(base, rel string) (string, error) {
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
	if err != nil || strings.HasPrefix(relToBase, "..") {
		return "", fmt.Errorf("path escapes work dir: %s", rel)
	}
	return abs, nil
}

// Static returns a fixed-root Service for tests.
func Static(root string) Service {
	return &staticService{root: filepath.Clean(root)}
}

type staticService struct {
	root string
}

func (s *staticService) Resolve(_ context.Context, rel string) (string, error) {
	return ResolveRel(s.root, rel)
}
