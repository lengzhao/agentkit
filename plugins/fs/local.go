package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type LocalConfig struct {
	// Root is directory relative to the workspace root; may use the global: or local: scope prefix.
	Root string `json:"root"`
}

type LocalDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

type localFS struct {
	relRoot   string
	workspace workspace.Service
}

// NewLocal registers fs/local: Local filesystem access, rooted inside the workspace.
//
// Best practices:
//   - Paths are resolved against the workspace, so a tool cannot escape root via ..
func NewLocal(cfg LocalConfig, deps LocalDeps) (filesystem.Service, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("fs/local requires workspace")
	}
	root := cfg.Root
	if root == "" {
		root = "."
	}
	return &localFS{relRoot: root, workspace: deps.Workspace}, nil
}

func (s *localFS) rootDir(ctx context.Context) (string, error) {
	return s.workspace.Resolve(ctx, s.relRoot)
}

func (s *localFS) resolve(ctx context.Context, path string) (string, error) {
	root, err := s.rootDir(ctx)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, string(filepath.Separator))
	}
	full := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes workspace: %s", path)
	}
	return full, nil
}

func (s *localFS) ReadText(ctx context.Context, path string, maxBytes int) (string, error) {
	full, err := s.resolve(ctx, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[:maxBytes]
	}
	return string(data), nil
}

func (s *localFS) WriteText(ctx context.Context, path, content string) error {
	full, err := s.resolve(ctx, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

func (s *localFS) Edit(ctx context.Context, req filesystem.EditRequest) (filesystem.EditResult, error) {
	content, err := s.ReadText(ctx, req.Path, 0)
	if err != nil {
		return filesystem.EditResult{}, err
	}
	if !strings.Contains(content, req.OldString) {
		return filesystem.EditResult{Path: req.Path}, nil
	}
	newContent := strings.Replace(content, req.OldString, req.NewString, 1)
	if err := s.WriteText(ctx, req.Path, newContent); err != nil {
		return filesystem.EditResult{}, err
	}
	return filesystem.EditResult{Path: req.Path, Applied: true}, nil
}
