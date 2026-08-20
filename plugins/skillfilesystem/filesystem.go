package skillfilesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/cap/skill"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	Dirs []string `json:"dirs"`
}

type Registry struct {
	dirs []string
}

func init() {
	pluginkit.Register("skill/filesystem", New)
}

func New(cfg Config) (skill.Registry, error) {
	dirs := cfg.Dirs
	if len(dirs) == 0 {
		dirs = []string{".cursor/skills", ".agents/skills", "skills"}
	}
	absDirs := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}
		absDirs = append(absDirs, abs)
	}
	return &Registry{dirs: absDirs}, nil
}

func (r *Registry) List(_ context.Context) ([]skill.Descriptor, error) {
	seen := make(map[string]struct{})
	var out []skill.Descriptor
	for _, dir := range r.dirs {
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

func (r *Registry) Load(_ context.Context, name string) (skill.Content, error) {
	for _, dir := range r.dirs {
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
