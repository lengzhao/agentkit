package fsreadonly

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	FS filesystem.Service `json:"fs"`
}

type Service struct {
	inner filesystem.Service
}

func init() {
	pluginkit.Register("fs/readonly", New)
}

func New(_ Config, deps Deps) (filesystem.Service, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("fs/readonly requires fs dependency")
	}
	return &Service{inner: deps.FS}, nil
}

func (s *Service) ReadText(ctx context.Context, path string, maxBytes int) (string, error) {
	return s.inner.ReadText(ctx, path, maxBytes)
}

func (s *Service) WriteText(context.Context, string, string) error {
	return fmt.Errorf("read-only filesystem")
}

func (s *Service) Edit(context.Context, filesystem.EditRequest) (filesystem.EditResult, error) {
	return filesystem.EditResult{}, fmt.Errorf("read-only filesystem")
}

func (s *Service) ListDir(ctx context.Context, path string) ([]filesystem.DirEntry, error) {
	return s.inner.ListDir(ctx, path)
}

func (s *Service) Grep(ctx context.Context, req filesystem.GrepRequest) (filesystem.GrepResult, error) {
	return s.inner.Grep(ctx, req)
}

func (s *Service) Find(ctx context.Context, req filesystem.FindRequest) (filesystem.FindResult, error) {
	return s.inner.Find(ctx, req)
}
