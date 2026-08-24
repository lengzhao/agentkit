package fs

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

func (s *memoryFS) ListDir(_ context.Context, path string) ([]filesystem.DirEntry, error) {
	dir := normalizePath(path)
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	var entries []filesystem.DirEntry
	for key := range s.files {
		rel, ok := childEntry(dir, key)
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
			Path:  joinPath(dir, name),
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
	searchPath := normalizePath(req.Path)
	maxMatches := req.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 100
	}
	pattern := req.Pattern
	if req.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return filesystem.GrepResult{}, fmt.Errorf("invalid pattern: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := filesystem.GrepResult{Matches: []filesystem.GrepMatch{}}
	for path, content := range s.files {
		if !pathUnder(searchPath, path) {
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
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			result.Matches = append(result.Matches, filesystem.GrepMatch{
				Path:    path,
				Line:    i + 1,
				Content: strings.TrimRight(line, "\r"),
			})
			if len(result.Matches) >= maxMatches {
				result.Truncated = true
				return result, nil
			}
		}
	}
	return result, nil
}

func (s *memoryFS) Find(_ context.Context, req filesystem.FindRequest) (filesystem.FindResult, error) {
	if req.Pattern == "" {
		return filesystem.FindResult{}, fmt.Errorf("pattern is required")
	}
	searchPath := normalizePath(req.Path)
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 200
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := filesystem.FindResult{Paths: []string{}}
	for path := range s.files {
		if !pathUnder(searchPath, path) {
			continue
		}
		matched, err := matchPattern(req.Pattern, path)
		if err != nil {
			return filesystem.FindResult{}, fmt.Errorf("invalid pattern: %w", err)
		}
		if !matched {
			continue
		}
		result.Paths = append(result.Paths, path)
		if len(result.Paths) >= maxResults {
			result.Truncated = true
			sort.Strings(result.Paths)
			return result, nil
		}
	}
	sort.Strings(result.Paths)
	return result, nil
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return ""
	}
	return strings.Trim(filepath.ToSlash(path), "/")
}

func joinPath(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

func childEntry(dir, key string) (string, bool) {
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

func pathUnder(dir, path string) bool {
	if dir == "" {
		return true
	}
	return path == dir || strings.HasPrefix(path, dir+"/")
}

func matchPattern(pattern, path string) (bool, error) {
	if strings.Contains(pattern, "/") {
		return filepath.Match(pattern, path)
	}
	return filepath.Match(pattern, filepath.Base(path))
}
