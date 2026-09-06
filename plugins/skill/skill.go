package skill

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/cap/skill"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtskill "github.com/lengzhao/agentkit/runtime/skill"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	// Dirs are directories to scan, in precedence order; each may use the global: or local: scope prefix.
	Dirs []string `json:"dirs"`
}

type Deps struct {
	Workspace workspace.Service `json:"workspace"`
}

type Registry struct {
	relDirs   []string
	workspace workspace.Service
}

func init() {
	pluginkit.Register("skill/filesystem", New)
}

// New registers skill/filesystem: Scan skill bundle directories for SKILL.md definitions.
func New(cfg Config, deps Deps) (skill.Registry, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("skill/filesystem requires workspace")
	}
	dirs := cfg.Dirs
	if len(dirs) == 0 {
		dirs = []string{"global:.cursor/skills", "global:.agents/skills", "global:skills"}
	}
	return &Registry{relDirs: dirs, workspace: deps.Workspace}, nil
}

func (r *Registry) List(ctx context.Context) ([]skill.Descriptor, error) {
	seen := make(map[string]struct{})
	var out []skill.Descriptor
	for _, rel := range r.relDirs {
		dir, err := r.workspace.Resolve(ctx, rel)
		if err != nil {
			continue
		}
		for _, candidate := range r.discoverDir(dir) {
			if _, ok := seen[candidate.Name]; ok {
				continue
			}
			seen[candidate.Name] = struct{}{}
			out = append(out, skill.Descriptor{
				Name:          candidate.Name,
				Description:   candidate.Description,
				Path:          candidate.ResourceDir,
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
		dir, err := r.workspace.Resolve(ctx, rel)
		if err != nil {
			continue
		}
		for _, candidate := range r.discoverDir(dir) {
			if candidate.Name != name {
				continue
			}
			return candidate.Content, nil
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

func (r *Registry) discoverDir(root string) []discoveredSkill {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []discoveredSkill
	for _, entry := range entries {
		if !entry.IsDir() {
			if strings.HasSuffix(entry.Name(), ".md") {
				slog.Warn("skill ignored: flat markdown files are not part of the Agent Skills directory layout", "path", filepath.Join(root, entry.Name()))
			}
			continue
		}
		skillPath := filepath.Join(root, entry.Name(), "SKILL.md")
		raw, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}
		parsed, err := rtskill.ParseFile(string(raw), rtskill.ParseOptions{DirName: entry.Name()})
		if err != nil {
			slog.Warn("skill ignored", "path", skillPath, "error", err)
			continue
		}
		if parsed.Content == "" {
			slog.Warn("skill ignored", "path", skillPath, "error", "empty body")
			continue
		}
		resourceDir := filepath.Join(root, entry.Name())
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
