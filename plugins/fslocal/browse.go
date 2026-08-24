package fslocal

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

var skipDirNames = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	".agent":       {},
}

func (s *Service) ListDir(ctx context.Context, path string) ([]filesystem.DirEntry, error) {
	full, err := s.resolve(ctx, path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", path)
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	out := make([]filesystem.DirEntry, 0, len(entries))
	for _, entry := range entries {
		rel := filepath.ToSlash(filepath.Join(path, entry.Name()))
		out = append(out, filesystem.DirEntry{
			Name:  entry.Name(),
			Path:  rel,
			IsDir: entry.IsDir(),
		})
	}
	return out, nil
}

func (s *Service) Grep(ctx context.Context, req filesystem.GrepRequest) (filesystem.GrepResult, error) {
	if req.Pattern == "" {
		return filesystem.GrepResult{}, fmt.Errorf("pattern is required")
	}
	searchPath := req.Path
	if searchPath == "" {
		searchPath = "."
	}
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

	root, err := s.resolve(ctx, searchPath)
	if err != nil {
		return filesystem.GrepResult{}, err
	}
	workspaceRoot, err := s.rootDir(ctx)
	if err != nil {
		return filesystem.GrepResult{}, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return filesystem.GrepResult{}, err
	}

	result := filesystem.GrepResult{Matches: []filesystem.GrepMatch{}}
	appendMatch := func(rel string, line int, content string) bool {
		result.Matches = append(result.Matches, filesystem.GrepMatch{
			Path:    rel,
			Line:    line,
			Content: strings.TrimRight(content, "\r\n"),
		})
		if len(result.Matches) >= maxMatches {
			result.Truncated = true
			return false
		}
		return true
	}

	if !rootInfo.IsDir() {
		if req.Glob != "" {
			matched, err := filepath.Match(req.Glob, filepath.Base(root))
			if err != nil {
				return filesystem.GrepResult{}, fmt.Errorf("invalid glob: %w", err)
			}
			if !matched {
				return result, nil
			}
		}
		rel, err := filepath.Rel(workspaceRoot, root)
		if err != nil {
			return filesystem.GrepResult{}, err
		}
		if err := grepFile(root, filepath.ToSlash(rel), re, appendMatch); err != nil {
			return filesystem.GrepResult{}, err
		}
		return result, nil
	}

	err = filepath.WalkDir(root, func(fullPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if fullPath != root {
				if _, skip := skipDirNames[entry.Name()]; skip {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if req.Glob != "" {
			matched, err := filepath.Match(req.Glob, entry.Name())
			if err != nil {
				return fmt.Errorf("invalid glob: %w", err)
			}
			if !matched {
				return nil
			}
		}
		rel, err := filepath.Rel(workspaceRoot, fullPath)
		if err != nil {
			return err
		}
		if result.Truncated {
			return filepath.SkipAll
		}
		return grepFile(fullPath, filepath.ToSlash(rel), re, appendMatch)
	})
	if err != nil {
		return filesystem.GrepResult{}, err
	}
	return result, nil
}

func grepFile(fullPath, rel string, re *regexp.Regexp, appendMatch func(string, int, string) bool) error {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if re.MatchString(line) {
			if !appendMatch(rel, lineNo, line) {
				return nil
			}
		}
	}
	return scanner.Err()
}

func (s *Service) Find(ctx context.Context, req filesystem.FindRequest) (filesystem.FindResult, error) {
	if req.Pattern == "" {
		return filesystem.FindResult{}, fmt.Errorf("pattern is required")
	}
	searchPath := req.Path
	if searchPath == "" {
		searchPath = "."
	}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 200
	}

	root, err := s.resolve(ctx, searchPath)
	if err != nil {
		return filesystem.FindResult{}, err
	}
	workspaceRoot, err := s.rootDir(ctx)
	if err != nil {
		return filesystem.FindResult{}, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return filesystem.FindResult{}, err
	}

	result := filesystem.FindResult{Paths: []string{}}
	appendPath := func(rel string) bool {
		result.Paths = append(result.Paths, rel)
		if len(result.Paths) >= maxResults {
			result.Truncated = true
			return false
		}
		return true
	}

	matchName := func(name, rel string) (bool, error) {
		if strings.Contains(req.Pattern, "/") {
			return filepath.Match(req.Pattern, rel)
		}
		return filepath.Match(req.Pattern, name)
	}

	if !rootInfo.IsDir() {
		rel, err := filepath.Rel(workspaceRoot, root)
		if err != nil {
			return filesystem.FindResult{}, err
		}
		rel = filepath.ToSlash(rel)
		matched, err := matchName(filepath.Base(root), rel)
		if err != nil {
			return filesystem.FindResult{}, fmt.Errorf("invalid pattern: %w", err)
		}
		if matched {
			appendPath(rel)
		}
		return result, nil
	}

	err = filepath.WalkDir(root, func(fullPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if fullPath != root {
				if _, skip := skipDirNames[entry.Name()]; skip {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(workspaceRoot, fullPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		matched, err := matchName(entry.Name(), rel)
		if err != nil {
			return fmt.Errorf("invalid pattern: %w", err)
		}
		if matched {
			if !appendPath(rel) {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return filesystem.FindResult{}, err
	}
	return result, nil
}
