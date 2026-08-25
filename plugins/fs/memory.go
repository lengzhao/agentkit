package fs

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

type MemoryConfig struct {
	// Files is seed contents, keyed by path.
	Files map[string]string `json:"files"`
}

type memoryFS struct {
	mu    sync.RWMutex
	files map[string]string
}

// NewMemory registers fs/memory: In-memory filesystem for tests and ephemeral sandboxes.
func NewMemory(cfg MemoryConfig) (filesystem.Service, error) {
	files := make(map[string]string, len(cfg.Files))
	for k, v := range cfg.Files {
		files[k] = v
	}
	return &memoryFS{files: files}, nil
}

func (s *memoryFS) ReadText(_ context.Context, path string, maxBytes int) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	content, ok := s.files[path]
	if !ok {
		return "", fmt.Errorf("file not found: %s", path)
	}
	if maxBytes > 0 && len(content) > maxBytes {
		content = content[:maxBytes]
	}
	return content, nil
}

func (s *memoryFS) WriteText(_ context.Context, path, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = make(map[string]string)
	}
	s.files[path] = content
	return nil
}

func (s *memoryFS) Edit(_ context.Context, req filesystem.EditRequest) (filesystem.EditResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, ok := s.files[req.Path]
	if !ok {
		return filesystem.EditResult{}, fmt.Errorf("file not found: %s", req.Path)
	}
	if !strings.Contains(content, req.OldString) {
		return filesystem.EditResult{Path: req.Path}, nil
	}
	s.files[req.Path] = strings.Replace(content, req.OldString, req.NewString, 1)
	return filesystem.EditResult{Path: req.Path, Applied: true}, nil
}
