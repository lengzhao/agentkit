package fs

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

type ReadonlyConfig struct{}

type ReadonlyDeps struct {
	FS filesystem.Service `json:"fs"`
}

type readonlyFS struct {
	inner filesystem.Service
}

func NewReadonly(_ ReadonlyConfig, deps ReadonlyDeps) (filesystem.Service, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("fs/readonly requires fs dependency")
	}
	return &readonlyFS{inner: deps.FS}, nil
}

func (s *readonlyFS) ReadText(ctx context.Context, path string, maxBytes int) (string, error) {
	return s.inner.ReadText(ctx, path, maxBytes)
}

func (s *readonlyFS) WriteText(context.Context, string, string) error {
	return fmt.Errorf("read-only filesystem")
}

func (s *readonlyFS) Edit(context.Context, filesystem.EditRequest) (filesystem.EditResult, error) {
	return filesystem.EditResult{}, fmt.Errorf("read-only filesystem")
}

func (s *readonlyFS) ListDir(ctx context.Context, path string) ([]filesystem.DirEntry, error) {
	return s.inner.ListDir(ctx, path)
}

func (s *readonlyFS) Grep(ctx context.Context, req filesystem.GrepRequest) (filesystem.GrepResult, error) {
	return s.inner.Grep(ctx, req)
}

func (s *readonlyFS) Find(ctx context.Context, req filesystem.FindRequest) (filesystem.FindResult, error) {
	return s.inner.Find(ctx, req)
}
