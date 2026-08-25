package subagent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"gopkg.in/yaml.v3"
)

// defaultDirs looks in the working directory first so a repo can ship its own
// child agents, then falls back to the user-global set.
var defaultDirs = []string{"local:agents", "global:agents"}

var (
	errNoDescription = errors.New("definition needs a description")
	errNoPrompt      = errors.New("definition needs a body to use as the system prompt")
)

// frontmatter is the YAML head of a definition file. Fields absent from the file
// fall back to the loader's defaults, so a two-line frontmatter is valid.
type frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
	Model       string   `yaml:"model"`
	MaxSteps    int      `yaml:"maxSteps"`
}

// loadDefinitions scans dirs in order and returns the definitions found, sorted
// by name. The first directory to define a name wins, which is what makes the
// working directory an override of the global set rather than a peer of it.
func loadDefinitions(ctx context.Context, ws workspace.Service, dirs []string) ([]subagent.Definition, error) {
	seen := make(map[string]struct{})
	var out []subagent.Definition
	for _, rel := range dirs {
		dir, err := ws.Resolve(ctx, rel)
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				slog.Warn("subagent definition unreadable", "path", path, "error", err)
				continue
			}
			def, err := parseDefinition(entry.Name(), string(data))
			if err != nil {
				// One malformed file must not take down delegation to every
				// other child agent, so it is skipped rather than fatal.
				slog.Warn("subagent definition ignored", "path", path, "error", err)
				continue
			}
			if _, ok := seen[def.Name]; ok {
				continue
			}
			seen[def.Name] = struct{}{}
			def.Path = path
			out = append(out, def)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parseDefinition reads one agents/<name>.md file: a YAML frontmatter block
// between --- lines, then the body, which becomes the child's system prompt.
func parseDefinition(fileName, raw string) (subagent.Definition, error) {
	head, body := splitFrontmatter(raw)
	var fm frontmatter
	if head != "" {
		if err := yaml.Unmarshal([]byte(head), &fm); err != nil {
			return subagent.Definition{}, err
		}
	}
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		name = strings.TrimSuffix(fileName, filepath.Ext(fileName))
	}
	description := strings.TrimSpace(fm.Description)
	if description == "" {
		// Without a description the parent has no basis for picking this agent;
		// listing it would only add noise to the system prompt.
		return subagent.Definition{}, errNoDescription
	}
	prompt := strings.TrimSpace(body)
	if prompt == "" {
		return subagent.Definition{}, errNoPrompt
	}
	return subagent.Definition{
		Name:        name,
		Description: description,
		Prompt:      prompt,
		Tools:       trimAll(fm.Tools),
		Model:       strings.TrimSpace(fm.Model),
		MaxSteps:    fm.MaxSteps,
	}, nil
}

// splitFrontmatter separates a leading --- delimited YAML block from the body.
// A file with no frontmatter, or with an unterminated one, is all body — the
// caller then reports the missing description rather than a YAML error.
func splitFrontmatter(raw string) (head, body string) {
	trimmed := strings.TrimLeft(raw, "\ufeff \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return "", raw
	}
	lines := strings.Split(trimmed, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n")
		}
	}
	return "", raw
}

func trimAll(in []string) []string {
	var out []string
	for _, item := range in {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
