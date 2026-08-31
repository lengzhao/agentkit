package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/media"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type FSWorkspaceConfig struct {
	// Root is directory relative to the workspace root; may use the global: or local: scope prefix.
	Root string `json:"root"`
	// MaxBytes is read truncation limit; defaults to 1 MiB.
	MaxBytes int `json:"maxBytes,omitempty"`
	// MaxMatches is grep cap per call; defaults to 100.
	MaxMatches int `json:"maxMatches,omitempty"`
	// MaxResults is find cap per call; defaults to 1000.
	MaxResults int `json:"maxResults,omitempty"`
	// MaxListEntries is ls cap per call; defaults to 500.
	MaxListEntries int `json:"maxListEntries,omitempty"`
	// ReadOnly rejects write and edit operations.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Tools limits which model tools are registered; empty means all six (read, write, edit, grep, find, ls).
	Tools []string `json:"tools,omitempty"`
}

type FSWorkspaceDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

type workspaceFS struct {
	relRoot   string
	workspace workspace.Service
	readOnly  bool
}

var _ workspaceFSOps = (*workspaceFS)(nil)

// NewFSWorkspace registers tool/fs-workspace: Workspace file tools (read, write, edit, grep, find, ls).
func NewFSWorkspace(cfg FSWorkspaceConfig, deps FSWorkspaceDeps) (agentkit.ToolPack, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/fs-workspace requires workspace")
	}
	root := cfg.Root
	if root == "" {
		root = "."
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
	fs := &workspaceFS{relRoot: root, workspace: deps.Workspace, readOnly: cfg.ReadOnly}
	return buildWorkspaceTools(fs, maxBytes, maxMatches, maxResults, maxListEntries, cfg.Tools)
}

// workspaceFSOps is the filesystem surface used by buildWorkspaceTools.
type workspaceFSOps interface {
	readText(ctx context.Context, path string, maxBytes int) (string, error)
	readImage(ctx context.Context, path string) (string, error)
	writeText(ctx context.Context, path, content string) error
	listDir(ctx context.Context, path string) ([]filesystem.DirEntry, error)
	grep(ctx context.Context, req filesystem.GrepRequest) (filesystem.GrepResult, error)
	find(ctx context.Context, req filesystem.FindRequest) (filesystem.FindResult, error)
}

func buildWorkspaceTools(fs workspaceFSOps, maxBytes, maxMatches, maxResults, maxListEntries int, only []string) (agentkit.ToolPack, error) {
	read, err := agentkit.NewTool[ReadInput, string]("read", func(ctx context.Context, input ReadInput) (string, error) {
		if media.IsImagePath(input.Path) {
			return fs.readImage(ctx, input.Path)
		}
		raw, err := fs.readText(ctx, input.Path, 0)
		if err != nil {
			return "", err
		}
		sliced, err := sliceReadContent(raw, readSliceOptions{
			MaxBytes: maxBytes,
			Offset:   input.Offset,
			Limit:    input.Limit,
		})
		if err != nil {
			return "", err
		}
		startLine := 1
		if input.Offset > 0 {
			startLine = input.Offset
		}
		return formatReadText(input.Path, startLine, sliced), nil
	}).Description("Read a text file from the workspace. Image files return inline vision data. Large files are truncated to 2000 lines or 50KB; use offset/limit to page through the rest.").Build()
	if err != nil {
		return nil, err
	}

	write, err := agentkit.NewTool[WriteInput, string]("write", func(ctx context.Context, input WriteInput) (string, error) {
		if err := fs.writeText(ctx, input.Path, input.Content); err != nil {
			return "", err
		}
		return formatWriteResult(input.Path), nil
	}).Description("Write content to a file in the workspace.").Build()
	if err != nil {
		return nil, err
	}

	edit, err := agentkit.NewTool[EditInput, string]("edit", applyWorkspaceEdits(fs)).
		Description("Make precise file edits with exact text replacement. Each edits[].oldText is matched against the original file, not incrementally. Do not emit overlapping edits.").Build()
	if err != nil {
		return nil, err
	}

	grep, err := agentkit.NewTool[GrepInput, string]("grep", func(ctx context.Context, input GrepInput) (string, error) {
		limit := effectiveLimit(input.Limit, maxMatches, defaultGrepLimit)
		result, err := fs.grep(ctx, filesystem.GrepRequest{
			Pattern:    input.Pattern,
			Path:       input.Path,
			Glob:       input.Glob,
			IgnoreCase: input.IgnoreCase,
			Literal:    input.Literal,
			Context:    input.Context,
			MaxMatches: limit,
		})
		if err != nil {
			return "", err
		}
		return formatGrepResult(result), nil
	}).Description("Search file contents in the workspace using a regular expression or literal string. Respects .gitignore.").Build()
	if err != nil {
		return nil, err
	}

	find, err := agentkit.NewTool[FindInput, string]("find", func(ctx context.Context, input FindInput) (string, error) {
		limit := effectiveLimit(input.Limit, maxResults, defaultFindLimit)
		result, err := fs.find(ctx, filesystem.FindRequest{
			Pattern:    input.Pattern,
			Path:       input.Path,
			MaxResults: limit,
		})
		if err != nil {
			return "", err
		}
		return formatFindResult(result), nil
	}).Description("Find files by glob pattern (e.g. *.go, **/*.json). Paths are relative to the search directory. Respects .gitignore.").Build()
	if err != nil {
		return nil, err
	}

	listDir, err := agentkit.NewTool[ListDirInput, string]("ls", func(ctx context.Context, input ListDirInput) (string, error) {
		entries, err := fs.listDir(ctx, input.Path)
		if err != nil {
			return "", err
		}
		limit := effectiveLimit(input.Limit, maxListEntries, defaultListLimit)
		truncated := len(entries) > limit
		if truncated {
			entries = entries[:limit]
		}
		text := formatListEntries(entries)
		hint := ""
		if truncated {
			hint = fmt.Sprintf("%d entries limit reached. Use limit=%d for more", limit, limit*2)
		}
		return formatListResult(text, hint), nil
	}).Description("List files and directories in a workspace path. Output includes dotfiles and marks directories with a trailing slash.").Build()
	if err != nil {
		return nil, err
	}

	return filterToolPack(agentkit.Pack(read, write, edit, grep, find, listDir), only), nil
}

func filterToolPack(pack agentkit.ToolPack, only []string) agentkit.ToolPack {
	if len(only) == 0 {
		return pack
	}
	allow := make(map[string]struct{}, len(only))
	for _, name := range only {
		allow[name] = struct{}{}
	}
	out := make(agentkit.ToolPack, 0, len(only))
	for _, tool := range pack {
		if _, ok := allow[tool.Name()]; ok {
			out = append(out, tool)
		}
	}
	return out
}

type ReadInput struct {
	Path   string `json:"path" jsonschema:"File path relative to the workspace"`
	Offset int    `json:"offset,omitempty" jsonschema:"Line number to start reading from (1-indexed)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of lines to read"`
}

type WriteInput struct {
	Path    string `json:"path" jsonschema:"File path relative to the workspace"`
	Content string `json:"content" jsonschema:"Full file content to write"`
}

type FileEdit struct {
	OldText string `json:"oldText" jsonschema:"Exact text to replace in the original file; must be unique and non-overlapping with other edits in the same call"`
	NewText string `json:"newText" jsonschema:"Replacement text"`
}

type EditInput struct {
	Path  string     `json:"path" jsonschema:"Path to the file to edit"`
	Edits []FileEdit `json:"edits" jsonschema:"One or more targeted replacements matched against the original file"`
}

type GrepInput struct {
	Pattern    string `json:"pattern" jsonschema:"Search pattern (regex or literal string)"`
	Path       string `json:"path,omitempty" jsonschema:"Directory or file to search (default: workspace root)"`
	Glob       string `json:"glob,omitempty" jsonschema:"Optional filename glob filter, e.g. *.go"`
	IgnoreCase bool   `json:"ignoreCase,omitempty" jsonschema:"Case-insensitive search"`
	Literal    bool   `json:"literal,omitempty" jsonschema:"Treat pattern as a literal string instead of a regex"`
	Context    int    `json:"context,omitempty" jsonschema:"Lines of context to show before and after each match"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum number of matches to return (default: 100)"`
}

type FindInput struct {
	Pattern string `json:"pattern" jsonschema:"Glob pattern to match files, e.g. *.go or **/*.json"`
	Path    string `json:"path,omitempty" jsonschema:"Directory to search (default: workspace root)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default: 1000)"`
}

type ListDirInput struct {
	Path  string `json:"path,omitempty" jsonschema:"Directory path relative to the workspace (default: root)"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum number of entries to return (default: 500)"`
}

func applyWorkspaceEdits(fs workspaceFSOps) func(context.Context, EditInput) (string, error) {
	return func(ctx context.Context, input EditInput) (string, error) {
		if input.Path == "" {
			return "", fmt.Errorf("path is required")
		}
		if len(input.Edits) == 0 {
			return "", fmt.Errorf("at least one edit is required")
		}
		content, err := fs.readText(ctx, input.Path, 0)
		if err != nil {
			return "", err
		}
		updated, err := applyEditsOnOriginal(content, input.Edits)
		if err != nil {
			return "", err
		}
		if updated == content {
			return formatEditResult(input.Path, false), nil
		}
		if err := fs.writeText(ctx, input.Path, updated); err != nil {
			return "", err
		}
		return formatEditResult(input.Path, true), nil
	}
}

func (s *workspaceFS) rootDir(ctx context.Context) (string, error) {
	return s.workspace.Resolve(ctx, s.relRoot)
}

func (s *workspaceFS) resolve(ctx context.Context, path string) (string, error) {
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

func (s *workspaceFS) readImage(ctx context.Context, path string) (string, error) {
	full, err := s.resolve(ctx, path)
	if err != nil {
		return "", err
	}
	return readImageToolResult(full, path)
}

func (s *workspaceFS) readText(ctx context.Context, path string, maxBytes int) (string, error) {
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

func (s *workspaceFS) writeText(ctx context.Context, path, content string) error {
	if s.readOnly {
		return fmt.Errorf("read-only filesystem")
	}
	full, err := s.resolve(ctx, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

func (s *workspaceFS) listDir(ctx context.Context, path string) ([]filesystem.DirEntry, error) {
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
		if path == "" || path == "." {
			rel = entry.Name()
		}
		out = append(out, filesystem.DirEntry{
			Name:  entry.Name(),
			Path:  rel,
			IsDir: entry.IsDir(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *workspaceFS) grep(ctx context.Context, req filesystem.GrepRequest) (filesystem.GrepResult, error) {
	if req.Pattern == "" {
		return filesystem.GrepResult{}, fmt.Errorf("pattern is required")
	}
	searchPath := req.Path
	if searchPath == "" {
		searchPath = "."
	}
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

	root, err := s.resolve(ctx, searchPath)
	if err != nil {
		return filesystem.GrepResult{}, err
	}
	workspaceRoot, err := s.rootDir(ctx)
	if err != nil {
		return filesystem.GrepResult{}, err
	}
	ignore, err := filesystem.LoadIgnoreMatcher(workspaceRoot)
	if err != nil {
		return filesystem.GrepResult{}, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return filesystem.GrepResult{}, err
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
				return filesystem.GrepResult{}, fmt.Errorf("invalid glob: %w", err)
			}
			if !matched {
				return collector.Result(), nil
			}
		}
		rel, err := filepath.Rel(workspaceRoot, root)
		if err != nil {
			return filesystem.GrepResult{}, err
		}
		if err := scanFile(root, filepath.ToSlash(rel)); err != nil {
			return filesystem.GrepResult{}, err
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
		return filesystem.GrepResult{}, err
	}
	return collector.Result(), nil
}

func (s *workspaceFS) find(ctx context.Context, req filesystem.FindRequest) (filesystem.FindResult, error) {
	if req.Pattern == "" {
		return filesystem.FindResult{}, fmt.Errorf("pattern is required")
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
		return filesystem.FindResult{}, err
	}
	workspaceRoot, err := s.rootDir(ctx)
	if err != nil {
		return filesystem.FindResult{}, err
	}
	ignore, err := filesystem.LoadIgnoreMatcher(workspaceRoot)
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
		if len(result.Paths) >= limit {
			result.Truncated = true
			return false
		}
		return true
	}

	if !rootInfo.IsDir() {
		rel, err := filepath.Rel(workspaceRoot, root)
		if err != nil {
			return filesystem.FindResult{}, err
		}
		rel = filepath.ToSlash(rel)
		matched, err := matchFilePattern(req.Pattern, rel)
		if err != nil {
			return filesystem.FindResult{}, fmt.Errorf("invalid pattern: %w", err)
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
		return filesystem.FindResult{}, err
	}
	sort.Strings(result.Paths)
	text, hint := formatFindPaths(result.Paths, result.Truncated, limit)
	result.Text = text
	result.Hint = hint
	return result, nil
}
