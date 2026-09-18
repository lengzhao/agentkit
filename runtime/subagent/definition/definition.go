// Package definition loads subagent definitions (agents/<name>.md files) and
// renders them for help output. It is split from runtime/subagent so consumers
// that only list or inspect definitions do not pull in the execution stack
// (runtime/agent, session stores, telemetry).
package definition

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/markdown"
	"gopkg.in/yaml.v3"
)

// defaultDirs looks in the working directory first so a repo can ship its own
// child agents, then falls back to the user-global set.
var defaultDirs = []string{"local:agents", "local:../examples/agents", "global:agents"}

// DefaultDirs returns the dirs scanned when config.dirs is empty.
func DefaultDirs() []string {
	return append([]string(nil), defaultDirs...)
}

// Load scans dirs in precedence order and returns subagent definitions.
func Load(ctx context.Context, ws workspace.Service, dirs []string) ([]subagent.Definition, error) {
	return load(ctx, ws, dirs)
}

// Find looks up a definition by name, case-insensitively.
func Find(defs []subagent.Definition, name string) (subagent.Definition, bool) {
	for _, def := range defs {
		if strings.EqualFold(def.Name, name) {
			return def, true
		}
	}
	return subagent.Definition{}, false
}

var (
	errNoDescription = errors.New("definition needs a description")
	errNoPrompt      = errors.New("definition needs a body to use as the system prompt")
	errLoopBackend   = errors.New("loop backend must be declared in subagent/loop-agent config, not agents/*.md")
)

// frontmatter is the YAML head of a definition file. Fields absent from the file
// fall back to the loader's defaults, so a two-line frontmatter is valid.
type frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Backend     string   `yaml:"backend"`
	Agent       string   `yaml:"agent"`
	Async       bool     `yaml:"async"`
	Tools       []string `yaml:"tools"`
	Skills      []string `yaml:"skills"`
	Model       string   `yaml:"model"`
	Modalities  []string `yaml:"modalities"`
}

// load scans dirs in order and returns the definitions found, sorted
// by name. The first directory to define a name wins, which is what makes the
// working directory an override of the global set rather than a peer of it.
func load(ctx context.Context, ws workspace.Service, dirs []string) ([]subagent.Definition, error) {
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
			def, err := parse(entry.Name(), string(data))
			if err != nil {
				if errors.Is(err, errLoopBackend) {
					continue
				}
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

// parse reads one agents/<name>.md file: a YAML frontmatter block
// between --- lines, then the body, which becomes the child's system prompt.
func parse(fileName, raw string) (subagent.Definition, error) {
	head, body, ok := markdown.Split(raw)
	if !ok {
		head, body = "", raw
	}
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
	backend := strings.TrimSpace(fm.Backend)
	if backend == "" {
		backend = subagent.BackendInprocess
	}
	if backend == subagent.BackendLoop {
		return subagent.Definition{}, errLoopBackend
	}
	prompt := strings.TrimSpace(body)
	if prompt == "" {
		return subagent.Definition{}, errNoPrompt
	}
	var modalities []string
	if len(fm.Modalities) > 0 {
		modalities = agentkit.NormalizeModalities(fm.Modalities)
	}
	return subagent.Definition{
		Name:        name,
		Description: description,
		Prompt:      prompt,
		Backend:     backend,
		Tools:       trimAll(fm.Tools),
		Skills:      trimAll(fm.Skills),
		Model:       strings.TrimSpace(fm.Model),
		Modalities:  modalities,
	}, nil
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
