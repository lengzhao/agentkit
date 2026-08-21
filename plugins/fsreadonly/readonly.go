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
