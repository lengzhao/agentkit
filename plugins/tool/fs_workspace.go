package tool

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type FSWorkspaceConfig struct {
	// Root is directory relative to the workspace root; may use the global: or local: scope prefix.
	Root string `json:"root"`
	// MaxBytes is read truncation limit; defaults to 1 MiB.
	MaxBytes int `json:"maxBytes,omitempty"`
	// MaxMatches is grep cap per call; defaults to 100.
	MaxMatches int `json:"maxMatches,omitempty"`
	// MaxResults is find cap per call; defaults to 200.
	MaxResults int `json:"maxResults,omitempty"`
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

var skipDirNames = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	".agent":       {},
}

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
		maxResults = 200
	}
	fs := &workspaceFS{relRoot: root, workspace: deps.Workspace, readOnly: cfg.ReadOnly}
	return buildWorkspaceTools(fs, maxBytes, maxMatches, maxResults, cfg.Tools)
}

// workspaceFSOps is the filesystem surface used by buildWorkspaceTools.
type workspaceFSOps interface {
	readText(ctx context.Context, path string, maxBytes int) (string, error)
	writeText(ctx context.Context, path, content string) error
	listDir(ctx context.Context, path string) ([]filesystem.DirEntry, error)
	grep(ctx context.Context, req filesystem.GrepRequest) (filesystem.GrepResult, error)
	find(ctx context.Context, req filesystem.FindRequest) (filesystem.FindResult, error)
}

func buildWorkspaceTools(fs workspaceFSOps, maxBytes, maxMatches, maxResults int, only []string) (agentkit.ToolPack, error) {
	read, err := agentkit.NewTool[ReadInput, ReadOutput]("read", func(ctx context.Context, input ReadInput) (ReadOutput, error) {
		content, err := fs.readText(ctx, input.Path, maxBytes)
		if err != nil {
			return ReadOutput{}, err
		}
		return ReadOutput{Content: content}, nil
	}).Description("Read a text file from the workspace.").Build()
	if err != nil {
		return nil, err
	}

	write, err := agentkit.NewTool[WriteInput, WriteOutput]("write", func(ctx context.Context, input WriteInput) (WriteOutput, error) {
		if err := fs.writeText(ctx, input.Path, input.Content); err != nil {
			return WriteOutput{}, err
		}
		return WriteOutput{Path: input.Path}, nil
	}).Description("Write content to a file in the workspace.").Build()
	if err != nil {
		return nil, err
	}

	edit, err := agentkit.NewTool("edit", applyWorkspaceEdits(fs)).
		Description("Make precise file edits with exact text replacement. Each oldText is matched against the original file.").Build()
	if err != nil {
		return nil, err
	}

	grep, err := agentkit.NewTool[GrepInput, GrepOutput]("grep", func(ctx context.Context, input GrepInput) (GrepOutput, error) {
		return fs.grep(ctx, filesystem.GrepRequest{
			Pattern:    input.Pattern,
			Path:       input.Path,
			Glob:       input.Glob,
			IgnoreCase: input.IgnoreCase,
			MaxMatches: maxMatches,
		})
	}).Description("Search file contents in the workspace using a regular expression.").Build()
	if err != nil {
		return nil, err
	}

	find, err := agentkit.NewTool[FindInput, FindOutput]("find", func(ctx context.Context, input FindInput) (FindOutput, error) {
		return fs.find(ctx, filesystem.FindRequest{
			Pattern:    input.Pattern,
			Path:       input.Path,
			MaxResults: maxResults,
		})
	}).Description("Find files in the workspace by filename glob pattern (e.g. *.go). Paths are matched relative to the search directory; ** recursive glob is not supported.").Build()
	if err != nil {
		return nil, err
	}

	listDir, err := agentkit.NewTool[ListDirInput, ListDirOutput]("ls", func(ctx context.Context, input ListDirInput) (ListDirOutput, error) {
		entries, err := fs.listDir(ctx, input.Path)
		if err != nil {
			return ListDirOutput{}, err
		}
		return ListDirOutput{Entries: entries}, nil
	}).Description("List files and directories in a workspace path.").Build()
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
	Path string `json:"path" jsonschema:"required,description=File path relative to the workspace"`
}

type ReadOutput struct {
	Content string `json:"content"`
}

type WriteInput struct {
	Path    string `json:"path" jsonschema:"required,description=File path relative to the workspace"`
	Content string `json:"content" jsonschema:"required,description=Full file content to write"`
}

type WriteOutput struct {
	Path string `json:"path"`
}

type FileEdit struct {
	OldText string `json:"oldText" jsonschema:"required,description=Exact text to replace in the original file"`
	NewText string `json:"newText" jsonschema:"required,description=Replacement text"`
}

type EditInput struct {
	Path  string     `json:"path" jsonschema:"required,description=Path to the file to edit"`
	Edits []FileEdit `json:"edits" jsonschema:"required"`
}

type EditOutput struct {
	Path    string `json:"path"`
	Applied bool   `json:"applied"`
}

type GrepInput struct {
	Pattern    string `json:"pattern" jsonschema:"required,description=Regular expression to search for"`
	Path       string `json:"path" jsonschema:"description=Directory or file to search (default: workspace root)"`
	Glob       string `json:"glob" jsonschema:"description=Optional filename glob filter, e.g. *.go"`
	IgnoreCase bool   `json:"ignoreCase" jsonschema:"description=Case-insensitive search"`
}

type GrepOutput = filesystem.GrepResult

type FindInput struct {
	Pattern string `json:"pattern" jsonschema:"required,description=Filename glob pattern, e.g. *.go"`
	Path    string `json:"path" jsonschema:"description=Directory to search (default: workspace root)"`
}

type FindOutput = filesystem.FindResult

type ListDirInput struct {
	Path string `json:"path" jsonschema:"description=Directory path relative to the workspace (default: root)"`
}

type ListDirOutput struct {
	Entries []filesystem.DirEntry `json:"entries"`
}

func applyWorkspaceEdits(fs workspaceFSOps) func(context.Context, EditInput) (EditOutput, error) {
	return func(ctx context.Context, input EditInput) (EditOutput, error) {
		if input.Path == "" {
			return EditOutput{}, fmt.Errorf("path is required")
		}
		if len(input.Edits) == 0 {
			return EditOutput{}, fmt.Errorf("at least one edit is required")
		}
		content, err := fs.readText(ctx, input.Path, 0)
		if err != nil {
			return EditOutput{}, err
		}
		for i, edit := range input.Edits {
			if edit.OldText == "" {
				return EditOutput{}, fmt.Errorf("edits[%d].oldText is required", i)
			}
			if !strings.Contains(content, edit.OldText) {
				return EditOutput{}, fmt.Errorf("edits[%d].oldText not found in file", i)
			}
			if strings.Count(content, edit.OldText) > 1 {
				return EditOutput{}, fmt.Errorf("edits[%d].oldText is not unique in file", i)
			}
		}
		updated := content
		applied := false
		for _, edit := range input.Edits {
			if !strings.Contains(updated, edit.OldText) {
				continue
			}
			updated = strings.Replace(updated, edit.OldText, edit.NewText, 1)
			applied = true
		}
		if updated == content {
			return EditOutput{Path: input.Path, Applied: false}, nil
		}
		if err := fs.writeText(ctx, input.Path, updated); err != nil {
			return EditOutput{}, err
		}
		return EditOutput{Path: input.Path, Applied: applied}, nil
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

func (s *workspaceFS) find(ctx context.Context, req filesystem.FindRequest) (filesystem.FindResult, error) {
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
