package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
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

var _ filesystem.Service = (*memoryFS)(nil)

// NewFSMemory registers tool/fs-memory: In-memory workspace file tools for tests and smoke runs.
func NewFSMemory(cfg FSMemoryConfig) (agentkit.ToolPack, error) {
	files := make(map[string]string, len(cfg.Files))
	for k, v := range cfg.Files {
		files[normalizeMemPath(k)] = v
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
	return buildWorkspaceTools(&fsAdapter{store: &memoryFS{files: files}}, maxBytes, maxMatches, maxResults, maxListEntries, cfg.Tools)
}

func (s *memoryFS) Read(_ context.Context, path string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	content, ok := s.files[normalizeMemPath(path)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", os.ErrNotExist, path)
	}
	return []byte(content), nil
}

func (s *memoryFS) Write(_ context.Context, path string, data []byte, _ ...filesystem.WriteOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = make(map[string]string)
	}
	s.files[normalizeMemPath(path)] = string(data)
	return nil
}

func (s *memoryFS) Append(_ context.Context, path string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = make(map[string]string)
	}
	key := normalizeMemPath(path)
	s.files[key] += string(data)
	return nil
}

func (s *memoryFS) Stat(_ context.Context, path string) (filesystem.Info, error) {
	p := normalizeMemPath(path)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if content, ok := s.files[p]; ok {
		name := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			name = p[i+1:]
		}
		if name == "" {
			name = p
		}
		return filesystem.Info{Name: name, Path: p, Size: int64(len(content))}, nil
	}
	prefix := p
	if prefix != "" {
		prefix += "/"
	}
	for key := range s.files {
		if p == "" || strings.HasPrefix(key, prefix) {
			name := p
			if name == "" {
				name = "."
			} else if i := strings.LastIndex(name, "/"); i >= 0 {
				name = name[i+1:]
			}
			return filesystem.Info{Name: name, Path: p, IsDir: true}, nil
		}
	}
	return filesystem.Info{}, os.ErrNotExist
}

func (s *memoryFS) List(_ context.Context, path string) ([]filesystem.DirEntry, error) {
	dir := normalizeMemPath(path)
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	var entries []filesystem.DirEntry
	for key := range s.files {
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

func (s *memoryFS) Grep(_ context.Context, req filesystem.GrepRequest) (filesystem.GrepResult, error) {
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

	s.mu.RLock()
	defer s.mu.RUnlock()

	collector := newGrepCollector(limit)
	for path, content := range s.files {
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

func (s *memoryFS) Find(_ context.Context, req filesystem.FindRequest) (filesystem.FindResult, error) {
	if req.Pattern == "" {
		return filesystem.FindResult{}, fmt.Errorf("pattern is required")
	}
	searchPath := normalizeMemPath(req.Path)
	limit := req.MaxResults
	if limit <= 0 {
		limit = defaultFindLimit
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := filesystem.FindResult{Paths: []string{}}
	for path := range s.files {
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
