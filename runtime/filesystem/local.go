package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	capfs "github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/workspace/workpath"
	"github.com/lengzhao/pluginkit"
)

// Config is filesystem/local: a workspace-rooted disk store.
type Config struct {
	// Root is directory relative to the workspace root; may use global: or local: prefix.
	Root string `json:"root"`
	// Unrestricted disables path confinement to Root (absolute paths and .. are allowed).
	Unrestricted bool `json:"unrestricted,omitempty"`
}

// Deps for filesystem/local.
type Deps struct {
	Workspace workspace.Service `json:"workspace"`
}

type localFS struct {
	relRoot      string
	workspace    workspace.Service
	unrestricted bool
}

func init() {
	pluginkit.Register("filesystem/local", New)
}

// New registers filesystem/local: local-disk cap/filesystem.Service over workspace.Resolve.
func New(cfg Config, deps Deps) (capfs.Service, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("filesystem/local requires workspace")
	}
	root := cfg.Root
	if root == "" {
		root = "."
	}
	return &localFS{
		relRoot:      root,
		workspace:    deps.Workspace,
		unrestricted: cfg.Unrestricted,
	}, nil
}

var _ capfs.Service = (*localFS)(nil)

func (s *localFS) rootDir(ctx context.Context) (string, error) {
	return s.workspace.Resolve(ctx, s.relRoot)
}

func (s *localFS) resolve(ctx context.Context, path string) (string, error) {
	// Scope-prefixed paths (global:/local:) are resolved by the workspace, which
	// owns the dual-root boundary (tenant workspaces reject ".." escapes). They
	// are not confined to this store's root.
	if _, _, scoped := workspace.ParseScoped(path); scoped {
		return s.workspace.Resolve(ctx, path)
	}
	if filepath.IsAbs(path) {
		clean := filepath.Clean(path)
		if s.unrestricted {
			return clean, nil
		}
		root, err := s.rootDir(ctx)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path escapes workspace: %s", path)
		}
		return clean, nil
	}
	clean := filepath.Clean(path)
	clean = workpath.TrimRedundantFSRootPrefix(s.relRoot, clean)
	root, err := s.rootDir(ctx)
	if err != nil {
		return "", err
	}
	full := filepath.Join(root, clean)
	if !s.unrestricted {
		rel, err := filepath.Rel(root, full)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path escapes workspace: %s", path)
		}
	}
	return full, nil
}

func (s *localFS) Read(ctx context.Context, path string) ([]byte, error) {
	full, err := s.resolve(ctx, path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func (s *localFS) Write(ctx context.Context, path string, data []byte, opts ...capfs.WriteOption) error {
	full, err := s.resolve(ctx, path)
	if err != nil {
		return err
	}
	var options capfs.WriteOptions
	for _, opt := range opts {
		opt(&options)
	}
	perm := options.Perm
	if perm == 0 {
		// Keep the existing file's mode; default new files to 0o644.
		if fi, statErr := os.Stat(full); statErr == nil {
			perm = fi.Mode().Perm()
		} else {
			perm = 0o644
		}
	}
	return WriteAtomic(full, data, perm)
}

func (s *localFS) Append(ctx context.Context, path string, data []byte) error {
	full, err := s.resolve(ctx, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func (s *localFS) Stat(ctx context.Context, path string) (capfs.Info, error) {
	full, err := s.resolve(ctx, path)
	if err != nil {
		return capfs.Info{}, err
	}
	fi, err := os.Stat(full)
	if err != nil {
		return capfs.Info{}, err
	}
	return capfs.Info{
		Name:    fi.Name(),
		Path:    path,
		Size:    fi.Size(),
		IsDir:   fi.IsDir(),
		ModTime: fi.ModTime(),
	}, nil
}

func (s *localFS) List(ctx context.Context, path string) ([]capfs.DirEntry, error) {
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
	out := make([]capfs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		rel := filepath.ToSlash(filepath.Join(path, entry.Name()))
		if path == "" || path == "." {
			rel = entry.Name()
		}
		var modTime time.Time
		if info, err := entry.Info(); err == nil {
			modTime = info.ModTime()
		}
		out = append(out, capfs.DirEntry{
			Name:    entry.Name(),
			Path:    rel,
			IsDir:   entry.IsDir(),
			ModTime: modTime,
		})
	}
	sortDirEntries(out)
	return out, nil
}

func (s *localFS) Grep(ctx context.Context, req capfs.GrepRequest) (capfs.GrepResult, error) {
	if req.Pattern == "" {
		return capfs.GrepResult{}, fmt.Errorf("pattern is required")
	}
	searchPath := req.Path
	if searchPath == "" {
		searchPath = "."
	}
	limit := req.MaxMatches
	if limit <= 0 {
		limit = defaultGrepLimit
	}
	re, err := compileGrep(req)
	if err != nil {
		return capfs.GrepResult{}, err
	}

	root, err := s.resolve(ctx, searchPath)
	if err != nil {
		return capfs.GrepResult{}, err
	}
	workspaceRoot, err := s.rootDir(ctx)
	if err != nil {
		return capfs.GrepResult{}, err
	}
	ignore, err := LoadIgnoreMatcher(workspaceRoot)
	if err != nil {
		return capfs.GrepResult{}, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return capfs.GrepResult{}, err
	}

	collector := newGrepCollector(limit)
	scanFile := func(fullPath, rel string) error {
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return err
		}
		return grepFileBytes(data, rel, req.Pattern, req.IgnoreCase, req.Literal, re, req.Context, collector)
	}

	if !rootInfo.IsDir() {
		if req.Glob != "" {
			matched, err := filepath.Match(req.Glob, filepath.Base(root))
			if err != nil {
				return capfs.GrepResult{}, fmt.Errorf("invalid glob: %w", err)
			}
			if !matched {
				return collector.Result(), nil
			}
		}
		rel, err := filepath.Rel(workspaceRoot, root)
		if err != nil {
			return capfs.GrepResult{}, err
		}
		if err := scanFile(root, filepath.ToSlash(rel)); err != nil {
			return capfs.GrepResult{}, err
		}
		return collector.Result(), nil
	}

	err = filepath.WalkDir(root, func(fullPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(workspaceRoot, fullPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if ignore.Ignored(rel, entry.IsDir()) {
			if entry.IsDir() && fullPath != root {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
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
		if collector.Truncated {
			return filepath.SkipAll
		}
		return scanFile(fullPath, rel)
	})
	if err != nil {
		return capfs.GrepResult{}, err
	}
	return collector.Result(), nil
}

func (s *localFS) Find(ctx context.Context, req capfs.FindRequest) (capfs.FindResult, error) {
	if req.Pattern == "" {
		return capfs.FindResult{}, fmt.Errorf("pattern is required")
	}
	searchPath := req.Path
	if searchPath == "" {
		searchPath = "."
	}
	limit := req.MaxResults
	if limit <= 0 {
		limit = defaultFindLimit
	}

	root, err := s.resolve(ctx, searchPath)
	if err != nil {
		return capfs.FindResult{}, err
	}
	workspaceRoot, err := s.rootDir(ctx)
	if err != nil {
		return capfs.FindResult{}, err
	}
	ignore, err := LoadIgnoreMatcher(workspaceRoot)
	if err != nil {
		return capfs.FindResult{}, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return capfs.FindResult{}, err
	}

	result := capfs.FindResult{Paths: []string{}}
	appendPath := func(rel string) bool {
		result.Paths = append(result.Paths, rel)
		if len(result.Paths) >= limit {
			result.Truncated = true
			return false
		}
		return true
	}

	if !rootInfo.IsDir() {
		rel, err := filepath.Rel(workspaceRoot, root)
		if err != nil {
			return capfs.FindResult{}, err
		}
		rel = filepath.ToSlash(rel)
		matched, err := matchFilePattern(req.Pattern, rel)
		if err != nil {
			return capfs.FindResult{}, fmt.Errorf("invalid pattern: %w", err)
		}
		if matched {
			appendPath(rel)
		}
		text, hint := formatFindPaths(result.Paths, result.Truncated, limit)
		result.Text = text
		result.Hint = hint
		return result, nil
	}

	err = filepath.WalkDir(root, func(fullPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(workspaceRoot, fullPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if ignore.Ignored(rel, entry.IsDir()) {
			if entry.IsDir() && fullPath != root {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		matched, err := matchFilePattern(req.Pattern, rel)
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
		return capfs.FindResult{}, err
	}
	sort.Strings(result.Paths)
	text, hint := formatFindPaths(result.Paths, result.Truncated, limit)
	result.Text = text
	result.Hint = hint
	return result, nil
}
