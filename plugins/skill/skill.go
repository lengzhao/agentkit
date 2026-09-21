package skill

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/skill"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	rtskill "github.com/lengzhao/agentkit/runtime/skill"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	// Dirs are directories to scan, in precedence order; each may use the global: or local: scope prefix.
	Dirs []string `json:"dirs"`
}

type Deps struct {
	FS        filesystem.Service `json:"fs"`
	Workspace workspace.Service  `json:"workspace"`
}

type Registry struct {
	relDirs   []string
	fs        filesystem.Service
	workspace workspace.Service
}

func init() {
	pluginkit.Register("skill/filesystem", New)
}

// New registers skill/filesystem: Scan skill bundle directories for SKILL.md definitions.
func New(cfg Config, deps Deps) (skill.Registry, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("skill/filesystem requires fs")
	}
	if deps.Workspace == nil {
		return nil, fmt.Errorf("skill/filesystem requires workspace")
	}
	dirs := cfg.Dirs
	if len(dirs) == 0 {
		dirs = []string{"global:.cursor/skills", "global:.agents/skills", "global:skills"}
	}
	return &Registry{relDirs: dirs, fs: deps.FS, workspace: deps.Workspace}, nil
}

func (r *Registry) List(ctx context.Context) ([]skill.Descriptor, error) {
	seen := make(map[string]struct{})
	var out []skill.Descriptor
	for _, rel := range r.relDirs {
		for _, candidate := range r.discoverDir(ctx, rel) {
			if _, ok := seen[candidate.Name]; ok {
				continue
			}
			seen[candidate.Name] = struct{}{}
			out = append(out, skill.Descriptor{
				Name:          candidate.Name,
				Description:   candidate.Description,
				Path:          r.agentPath(ctx, candidate.ResourceDir),
				License:       candidate.License,
				Compatibility: candidate.Compatibility,
			})
		}
	}
	return out, nil
}

func (r *Registry) Load(ctx context.Context, name string) (skill.Content, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return skill.Content{}, fmt.Errorf("skill name is required")
	}
	for _, rel := range r.relDirs {
		for _, candidate := range r.discoverDir(ctx, rel) {
			if candidate.Name != name {
				continue
			}
			c := candidate.Content
			c.Path = r.agentPath(ctx, candidate.ResourceDir)
			return c, nil
		}
	}
	return skill.Content{}, fmt.Errorf("skill %q not found", name)
}

type discoveredSkill struct {
	Name          string
	Description   string
	ResourceDir   string
	License       string
	Compatibility string
	Content       skill.Content
}

// discoverDir scans one fs-relative directory (may carry a global:/local: scope
// prefix). Agent-facing paths are absolute host paths via workspace.Resolve.
func (r *Registry) discoverDir(ctx context.Context, root string) []discoveredSkill {
	entries, err := r.fs.List(ctx, root)
	if err != nil {
		return nil
	}
	base := strings.TrimSuffix(root, "/")
	var out []discoveredSkill
	for _, entry := range entries {
		if !entry.IsDir {
			if strings.HasSuffix(entry.Name, ".md") {
				slog.Warn("skill ignored: flat markdown files are not part of the Agent Skills directory layout", "path", base+"/"+entry.Name)
			}
			continue
		}
		skillPath := base + "/" + entry.Name + "/SKILL.md"
		raw, err := r.fs.Read(ctx, skillPath)
		if err != nil {
			continue
		}
		parsed, err := rtskill.ParseFile(string(raw), rtskill.ParseOptions{DirName: entry.Name})
		if err != nil {
			slog.Warn("skill ignored", "path", skillPath, "error", err)
			continue
		}
		if parsed.Content == "" {
			slog.Warn("skill ignored", "path", skillPath, "error", "empty body")
			continue
		}
		resourceDir := base + "/" + entry.Name
		out = append(out, discoveredSkill{
			Name:          parsed.Name,
			Description:   parsed.Description,
			ResourceDir:   resourceDir,
			License:       parsed.License,
			Compatibility: parsed.Compatibility,
			Content: skill.Content{
				Name:          parsed.Name,
				Description:   parsed.Description,
				Body:          parsed.Content,
				Path:          resourceDir,
				License:       parsed.License,
				Compatibility: parsed.Compatibility,
				AllowedTools:  parsed.AllowedTools,
				Metadata:      parsed.Metadata,
			},
		})
	}
	return out
}

func (r *Registry) agentPath(ctx context.Context, logical string) string {
	logical = strings.TrimSpace(logical)
	if logical == "" {
		return logical
	}
	abs, err := r.workspace.Resolve(ctx, logical)
	if err != nil {
		return rtmedia.AgentLLMPath(ctx, r.workspace, logical)
	}
	return filepath.Clean(abs)
}
