package fslocal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	Root string `json:"root"`
}

type Deps struct {
	Workspace workspace.Service `json:"workspace"`
}

type Service struct {
	relRoot   string
	workspace workspace.Service
}

func init() {
	pluginkit.Register("fs/local", New)
}

func New(cfg Config, deps Deps) (filesystem.Service, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("fs/local requires workspace")
	}
	root := cfg.Root
	if root == "" {
		root = "."
	}
	return &Service{relRoot: root, workspace: deps.Workspace}, nil
}

func (s *Service) rootDir(ctx context.Context) (string, error) {
	return s.workspace.Resolve(ctx, s.relRoot)
}

func (s *Service) resolve(ctx context.Context, path string) (string, error) {
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

func (s *Service) ReadText(ctx context.Context, path string, maxBytes int) (string, error) {
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

func (s *Service) WriteText(ctx context.Context, path, content string) error {
	full, err := s.resolve(ctx, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

func (s *Service) Edit(ctx context.Context, req filesystem.EditRequest) (filesystem.EditResult, error) {
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
