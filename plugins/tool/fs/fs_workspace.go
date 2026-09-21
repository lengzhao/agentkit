package fs

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	"github.com/lengzhao/agentkit/runtime/workspace/workpath"
)

type FSWorkspaceConfig struct {
	// Root is accepted for older configs; path confinement lives on filesystem/local.
	Root string `json:"root,omitempty"`
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
	// Unrestricted is accepted for older configs; belongs on filesystem/local.
	Unrestricted bool `json:"unrestricted,omitempty"`
	// Tools limits which model tools are registered; empty means all six (read, write, edit, grep, find, ls).
	Tools []string `json:"tools,omitempty"`
}

type FSWorkspaceDeps struct {
	FS        filesystem.Service `json:"fs"`
	Workspace workspace.Service  `json:"workspace,omitempty"`
}

type fsAdapter struct {
	store     filesystem.Service
	workspace workspace.Service
	readOnly  bool
}

var _ workspaceFSOps = (*fsAdapter)(nil)

// NewFSWorkspace registers tool/fs-workspace: model file tools over an injected filesystem.Service.
func NewFSWorkspace(cfg FSWorkspaceConfig, deps FSWorkspaceDeps) (agentkit.ToolPack, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/fs-workspace requires fs (filesystem/local or another filesystem.Service)")
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
	fs := &fsAdapter{
		store:     deps.FS,
		workspace: deps.Workspace,
		readOnly:  cfg.ReadOnly,
	}
	return buildWorkspaceTools(fs, maxBytes, maxMatches, maxResults, maxListEntries, cfg.Tools)
}

// workspaceFSOps is the filesystem surface used by buildWorkspaceTools.
type workspaceFSOps interface {
	readText(ctx context.Context, path string, maxBytes int) (string, error)
	readImage(ctx context.Context, path string) (string, error)
	pathMayBeImage(ctx context.Context, path string) bool
	writeText(ctx context.Context, path, content string) error
	listDir(ctx context.Context, path string) ([]filesystem.DirEntry, error)
	grep(ctx context.Context, req filesystem.GrepRequest) (filesystem.GrepResult, error)
	find(ctx context.Context, req filesystem.FindRequest) (filesystem.FindResult, error)
	llmPath(ctx context.Context, path string) string
	resolveToolPath(ctx context.Context, path string) (string, error)
	rewritePathsInText(ctx context.Context, text string) string
}

func buildWorkspaceTools(fs workspaceFSOps, maxBytes, maxMatches, maxResults, maxListEntries int, only []string) (agentkit.ToolPack, error) {
	read, err := agentkit.NewTool[ReadInput, string]("read", func(ctx context.Context, input ReadInput) (string, error) {
		if fs.pathMayBeImage(ctx, input.Path) {
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
		return formatReadText(fs.llmPath(ctx, input.Path), startLine, sliced), nil
	}).Description("Read a text file. path accepts absolute paths, relative ./..., or paths relative to the workspace root (e.g. under work/). Image files return metadata and are loaded for vision before the next model step. Large text files are truncated to 2000 lines or 50KB; use offset/limit to page through the rest.").Build()
	if err != nil {
		return nil, err
	}

	write, err := agentkit.NewTool[WriteInput, string]("write", func(ctx context.Context, input WriteInput) (string, error) {
		if err := fs.writeText(ctx, input.Path, input.Content); err != nil {
			return "", err
		}
		return formatWriteResult(fs.llmPath(ctx, input.Path)), nil
	}).Description("Write content to a file. path accepts absolute paths, relative ./..., or paths relative to the workspace root (e.g. under work/).").Build()
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
		return fs.rewritePathsInText(ctx, formatGrepResult(result)), nil
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
		return fs.rewritePathsInText(ctx, formatFindResult(result)), nil
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
		return fs.rewritePathsInText(ctx, formatListResult(text, hint)), nil
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
	Path   string `json:"path" jsonschema:"Absolute filesystem path; paths under the agent work directory may be given relative to work (tool output always uses absolute paths)"`
	Offset int    `json:"offset,omitempty" jsonschema:"Line number to start reading from (1-indexed)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of lines to read"`
}

type WriteInput struct {
	Path    string `json:"path" jsonschema:"Absolute filesystem path; paths under the agent work directory may be given relative to work (tool output always uses absolute paths)"`
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
		resolved, err := fs.resolveToolPath(ctx, input.Path)
		if err != nil {
			return "", err
		}
		content, err := fs.readText(ctx, resolved, 0)
		if err != nil {
			return "", err
		}
		updated, err := applyEditsOnOriginal(content, input.Edits)
		if err != nil {
			return "", err
		}
		if updated == content {
			return formatEditResult(fs.llmPath(ctx, resolved), false), nil
		}
		if err := fs.writeText(ctx, resolved, updated); err != nil {
			return "", err
		}
		return formatEditResult(fs.llmPath(ctx, resolved), true), nil
	}
}

func (a *fsAdapter) llmPath(ctx context.Context, path string) string {
	if a.workspace == nil {
		return path
	}
	return rtmedia.AgentLLMPath(ctx, a.workspace, path)
}

func (a *fsAdapter) rewritePathsInText(ctx context.Context, text string) string {
	if a.workspace == nil {
		return text
	}
	return rtmedia.RewritePathsInText(ctx, a.workspace, text)
}

func (a *fsAdapter) readImage(ctx context.Context, path string) (string, error) {
	resolved, err := a.resolveToolPath(ctx, path)
	if err != nil {
		return "", err
	}
	display := a.llmPath(ctx, resolved)
	if display == "" {
		display = resolved
	}
	return readImageToolResult(ctx, a.store, resolved, display)
}

func (a *fsAdapter) pathMayBeImage(ctx context.Context, path string) bool {
	if rtmedia.IsImagePath(path) {
		return true
	}
	resolved, err := a.resolveToolPath(ctx, path)
	if err != nil {
		return false
	}
	info, err := a.store.Stat(ctx, resolved)
	if err != nil || info.IsDir {
		return false
	}
	data, err := a.store.Read(ctx, resolved)
	if err != nil {
		return false
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	return rtmedia.LooksLikeImageData(head)
}

func (a *fsAdapter) resolveToolPath(ctx context.Context, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if a.workspace == nil {
		return rtmedia.AgentLLMPath(ctx, nil, path), nil
	}
	return workpath.AbsolutePath(ctx, a.workspace, path)
}

func (a *fsAdapter) readText(ctx context.Context, path string, maxBytes int) (string, error) {
	path, err := a.resolveToolPath(ctx, path)
	if err != nil {
		return "", err
	}
	data, err := a.store.Read(ctx, path)
	if err != nil {
		return "", err
	}
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[:maxBytes]
	}
	return string(data), nil
}

func (a *fsAdapter) writeText(ctx context.Context, path, content string) error {
	if a.readOnly {
		return fmt.Errorf("read-only filesystem")
	}
	path, err := a.resolveToolPath(ctx, path)
	if err != nil {
		return err
	}
	return a.store.Write(ctx, path, []byte(content))
}

func (a *fsAdapter) listDir(ctx context.Context, path string) ([]filesystem.DirEntry, error) {
	if strings.TrimSpace(path) != "" {
		resolved, err := a.resolveToolPath(ctx, path)
		if err != nil {
			return nil, err
		}
		path = resolved
	}
	return a.store.List(ctx, path)
}

func (a *fsAdapter) grep(ctx context.Context, req filesystem.GrepRequest) (filesystem.GrepResult, error) {
	if strings.TrimSpace(req.Path) != "" {
		resolved, err := a.resolveToolPath(ctx, req.Path)
		if err != nil {
			return filesystem.GrepResult{}, err
		}
		req.Path = resolved
	}
	return a.store.Grep(ctx, req)
}

func (a *fsAdapter) find(ctx context.Context, req filesystem.FindRequest) (filesystem.FindResult, error) {
	if strings.TrimSpace(req.Path) != "" {
		resolved, err := a.resolveToolPath(ctx, req.Path)
		if err != nil {
			return filesystem.FindResult{}, err
		}
		req.Path = resolved
	}
	return a.store.Find(ctx, req)
}
