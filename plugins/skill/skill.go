package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/cap/skill"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
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
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if _, ok := seen[name]; ok {
				continue
			}
			skillPath := filepath.Join(dir, name, "SKILL.md")
			data, err := os.ReadFile(skillPath)
			if err != nil {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, skill.Descriptor{
				Name:        name,
				Description: parseDescription(string(data)),
				Path:        filepath.Join(dir, name),
			})
		}
	}
	return out, nil
}

func (r *Registry) Load(ctx context.Context, name string) (skill.Content, error) {
	for _, rel := range r.relDirs {
		dir, err := r.workspace.Resolve(ctx, rel)
		if err != nil {
			continue
		}
		skillDir := filepath.Join(dir, name)
		skillPath := filepath.Join(skillDir, "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}
		body := strings.TrimSpace(string(data))
		if body == "" {
			return skill.Content{}, fmt.Errorf("skill %q is empty", name)
		}
		return skill.Content{
			Name:        name,
			Description: parseDescription(body),
			Body:        body,
			Path:        skillDir,
		}, nil
	}
	return skill.Content{}, fmt.Errorf("skill %q not found", name)
}

func parseDescription(body string) string {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	if len(lines) > 0 {
		return strings.TrimPrefix(strings.TrimSpace(lines[0]), "#")
	}
	return ""
}
