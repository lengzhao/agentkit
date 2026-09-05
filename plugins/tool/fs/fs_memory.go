package fs

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

type FSMemoryConfig struct {
	// Files is seed contents, keyed by path.
	Files map[string]string `json:"files"`
	// MaxBytes is read truncation limit; defaults to 1 MiB.
	MaxBytes int `json:"maxBytes,omitempty"`
	// MaxMatches is grep cap per call; defaults to 100.
	MaxMatches int `json:"maxMatches,omitempty"`
	// MaxResults is find cap per call; defaults to 1000.
	MaxResults int `json:"maxResults,omitempty"`
	// MaxListEntries is ls cap per call; defaults to 500.
	MaxListEntries int `json:"maxListEntries,omitempty"`
	// Tools limits which model tools are registered; empty means all six.
	Tools []string `json:"tools,omitempty"`
}

type memoryFS struct {
	mu    sync.RWMutex
	files map[string]string
}

// NewFSMemory registers tool/fs-memory: In-memory workspace file tools for tests and smoke runs.
func NewFSMemory(cfg FSMemoryConfig) (agentkit.ToolPack, error) {
	files := make(map[string]string, len(cfg.Files))
	for k, v := range cfg.Files {
		files[k] = v
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	maxMatches := cfg.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 100
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultFindLimit
	}
	maxListEntries := cfg.MaxListEntries
	if maxListEntries <= 0 {
		maxListEntries = defaultListLimit
	}
	fs := &memoryWorkspaceFS{inner: &memoryFS{files: files}}
	return buildWorkspaceTools(fs, maxBytes, maxMatches, maxResults, maxListEntries, cfg.Tools)
}

// memoryWorkspaceFS adapts memoryFS to workspaceFS operations.
type memoryWorkspaceFS struct {
	inner *memoryFS
}

func (s *memoryWorkspaceFS) readText(_ context.Context, path string, maxBytes int) (string, error) {
	s.inner.mu.RLock()
	defer s.inner.mu.RUnlock()
	content, ok := s.inner.files[normalizeMemPath(path)]
	if !ok {
		return "", fmt.Errorf("file not found: %s", path)
	}
	if maxBytes > 0 && len(content) > maxBytes {
		content = content[:maxBytes]
	}
	return content, nil
}

func (s *memoryWorkspaceFS) readImage(_ context.Context, path string) (string, error) {
	s.inner.mu.RLock()
	defer s.inner.mu.RUnlock()
	content, ok := s.inner.files[normalizeMemPath(path)]
	if !ok {
		return "", fmt.Errorf("file not found: %s", path)
	}
	data := []byte(content)
	if len(data) > rtmedia.DefaultMaxWorkspaceImageBytes {
		return rtmedia.FormatReadImageTooLarge(path, int64(len(data)), rtmedia.DefaultMaxWorkspaceImageBytes), nil
	}
	return rtmedia.FormatReadImageResult(path, rtmedia.DetectMIME(path, data), int64(len(data))), nil
}

func (s *memoryWorkspaceFS) writeText(_ context.Context, path, content string) error {
	s.inner.mu.Lock()
	defer s.inner.mu.Unlock()
	if s.inner.files == nil {
		s.inner.files = make(map[string]string)
	}
	s.inner.files[normalizeMemPath(path)] = content
	return nil
}

func (s *memoryWorkspaceFS) listDir(_ context.Context, path string) ([]filesystem.DirEntry, error) {
	dir := normalizeMemPath(path)
	s.inner.mu.RLock()
	defer s.inner.mu.RUnlock()

	seen := make(map[string]struct{})
	var entries []filesystem.DirEntry
	for key := range s.inner.files {
		rel, ok := childMemEntry(dir, key)
		if !ok {
			continue
		}
		if _, exists := seen[rel]; exists {
			continue
		}
		seen[rel] = struct{}{}
		name := rel
		isDir := false
		if idx := strings.Index(rel, "/"); idx >= 0 {
			name = rel[:idx]
			isDir = true
		}
		entries = append(entries, filesystem.DirEntry{
			Name:  name,
			Path:  joinMemPath(dir, name),
			IsDir: isDir,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func (s *memoryWorkspaceFS) grep(_ context.Context, req filesystem.GrepRequest) (filesystem.GrepResult, error) {
	if req.Pattern == "" {
		return filesystem.GrepResult{}, fmt.Errorf("pattern is required")
	}
	searchPath := normalizeMemPath(req.Path)
	limit := req.MaxMatches
	if limit <= 0 {
		limit = defaultGrepLimit
	}

	var re *regexp.Regexp
	if !req.Literal {
		pattern := req.Pattern
		if req.IgnoreCase {
			pattern = "(?i)" + pattern
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return filesystem.GrepResult{}, fmt.Errorf("invalid pattern: %w", err)
		}
		re = compiled
	}

	s.inner.mu.RLock()
	defer s.inner.mu.RUnlock()

	collector := newGrepCollector(limit)
	for path, content := range s.inner.files {
		if !pathUnderMem(searchPath, path) {
			continue
		}
		if req.Glob != "" {
			matched, err := filepath.Match(req.Glob, filepath.Base(path))
			if err != nil {
				return filesystem.GrepResult{}, fmt.Errorf("invalid glob: %w", err)
			}
			if !matched {
				continue
			}
		}
		if err := grepFileBytes([]byte(content), path, req.Pattern, req.IgnoreCase, req.Literal, re, req.Context, collector); err != nil {
			return filesystem.GrepResult{}, err
		}
		if collector.Truncated {
			break
		}
	}
	return collector.Result(), nil
}

func (s *memoryWorkspaceFS) find(_ context.Context, req filesystem.FindRequest) (filesystem.FindResult, error) {
	if req.Pattern == "" {
		return filesystem.FindResult{}, fmt.Errorf("pattern is required")
	}
	searchPath := normalizeMemPath(req.Path)
	limit := req.MaxResults
	if limit <= 0 {
		limit = defaultFindLimit
	}

	s.inner.mu.RLock()
	defer s.inner.mu.RUnlock()

	result := filesystem.FindResult{Paths: []string{}}
	for path := range s.inner.files {
		if !pathUnderMem(searchPath, path) {
			continue
		}
		matched, err := matchFilePattern(req.Pattern, path)
		if err != nil {
			return filesystem.FindResult{}, fmt.Errorf("invalid pattern: %w", err)
		}
		if !matched {
			continue
		}
		result.Paths = append(result.Paths, path)
		if len(result.Paths) >= limit {
			result.Truncated = true
			break
		}
	}
	sort.Strings(result.Paths)
	text, hint := formatFindPaths(result.Paths, result.Truncated, limit)
	result.Text = text
	result.Hint = hint
	return result, nil
}

func normalizeMemPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return ""
	}
	return strings.Trim(filepath.ToSlash(path), "/")
}

func joinMemPath(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

func childMemEntry(dir, key string) (string, bool) {
	if dir == "" {
		if strings.Contains(key, "/") {
			return strings.SplitN(key, "/", 2)[0], true
		}
		return key, true
	}
	prefix := dir + "/"
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	if rest == "" {
		return "", false
	}
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx], true
	}
	return rest, true
}

func pathUnderMem(dir, path string) bool {
	if dir == "" {
		return true
	}
	return path == dir || strings.HasPrefix(path, dir+"/")
}
