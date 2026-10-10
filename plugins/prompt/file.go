package prompt

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// FileConfig configures prompt/section/file: inject the contents of one or
// more workspace files into the system prompt.
type FileConfig struct {
	// Name is the section label, used for ordering and debugging.
	Name string `json:"name"`
	// Root is the workspace-relative directory to start reading from.
	Root string `json:"root"`
	// Filenames are candidate files (names or relative paths) read in each
	// visited directory; missing or empty files are silently skipped.
	Filenames []string `json:"filenames"`
	// WalkUp continues the search in parent directories up to the filesystem
	// root. Wire an unrestricted filesystem/local instance: the upward walk
	// passes absolute host paths; non-local backends simply miss every
	// candidate and inject nothing.
	WalkUp bool `json:"walkUp"`
}

// FileDeps are the dependencies of prompt/section/file.
type FileDeps struct {
	Workspace workspace.Service `json:"workspace"`
	// FS reads candidate files; see FileConfig.WalkUp for why an unrestricted
	// filesystem/local instance is recommended.
	FS filesystem.Service `json:"fs"`
}

// SetDefaults implements pluginkit.Defaulter.
func (c *FileConfig) SetDefaults() {
	if c.Name == "" {
		c.Name = "file"
	}
	if c.Root == "" {
		c.Root = "."
	}
}

type fileProvider struct {
	name      string
	relRoot   string
	filenames []string
	walkUp    bool
	workspace workspace.Service
	fs        filesystem.Service
}

// NewFile registers prompt/section/file: inject file contents discovered from
// the workspace directory into the system prompt.
func NewFile(cfg FileConfig, deps FileDeps) (agentkit.SectionProvider, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("prompt/section/file requires workspace")
	}
	if deps.FS == nil {
		return nil, fmt.Errorf("prompt/section/file requires fs")
	}
	if len(cfg.Filenames) == 0 {
		return nil, fmt.Errorf("prompt/section/file requires at least one filename")
	}
	cfg.SetDefaults()
	return newFileProvider(cfg.Name, cfg.Root, cfg.Filenames, cfg.WalkUp, deps.Workspace, deps.FS), nil
}

func newFileProvider(name, relRoot string, filenames []string, walkUp bool, ws workspace.Service, fs filesystem.Service) *fileProvider {
	return &fileProvider{
		name:      name,
		relRoot:   relRoot,
		filenames: filenames,
		walkUp:    walkUp,
		workspace: ws,
		fs:        fs,
	}
}

func (p *fileProvider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  p.name,
		Build: p.build,
	}}
}

func (p *fileProvider) build(ctx context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	root, err := p.workspace.Resolve(ctx, p.relRoot)
	if err != nil {
		return agentkit.PromptSection{}, err
	}
	var parts []string
	dir := root
	for {
		for _, name := range p.filenames {
			path := filepath.Join(dir, name)
			data, err := p.fs.Read(ctx, path)
			if err == nil && len(strings.TrimSpace(string(data))) > 0 {
				parts = append(parts, string(data))
			}
		}
		if !p.walkUp {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return agentkit.PromptSection{
		Name:    p.name,
		Content: strings.Join(parts, "\n\n"),
	}, nil
}
