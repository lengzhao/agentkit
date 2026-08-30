package prompt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/learning"
)

type MemoryMDConfig struct {
	// Root is directory to start the upward search from.
	Root string `json:"root"`
	// Filenames overrides the default memory files to search in each directory.
	Filenames []string `json:"filenames"`
}

type MemoryMDDeps struct {
	Workspace workspace.Service `json:"workspace"`
	// Learning wires /learn into the build graph and shares memory.md with this section.
	Learning *learning.Service `json:"learning"`
}

type memoryMDProvider struct {
	relRoot   string
	filenames []string
	workspace workspace.Service
}

// NewMemoryMD registers prompt/section/memory: Inject memory.md instructions discovered in the workspace hierarchy.
func NewMemoryMD(cfg MemoryMDConfig, deps MemoryMDDeps) (agentkit.SectionProvider, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("prompt/section/memory requires workspace")
	}
	if deps.Learning == nil {
		return nil, fmt.Errorf("prompt/section/memory requires learning")
	}
	root := cfg.Root
	if root == "" {
		root = "."
	}
	filenames := cfg.Filenames
	if len(filenames) == 0 {
		filenames = defaultMemoryMDFilenames()
	}
	return &memoryMDProvider{
		relRoot:   root,
		workspace: deps.Workspace,
		filenames: filenames,
	}, nil
}

func defaultMemoryMDFilenames() []string {
	return []string{"memory.md", "MEMORY.md"}
}

func (p *memoryMDProvider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  "memory",
		Build: p.build,
	}}
}

func (p *memoryMDProvider) build(ctx context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	root, err := p.workspace.Resolve(ctx, p.relRoot)
	if err != nil {
		return agentkit.PromptSection{}, err
	}
	var parts []string
	seen := map[string]bool{}
	dir := root
	for {
		for _, name := range p.filenames {
			path := filepath.Join(dir, name)
			key := strings.ToLower(path)
			if seen[key] {
				continue
			}
			seen[key] = true
			data, err := os.ReadFile(path)
			if err == nil {
				if content := formatMemoryFileContent(data); content != "" {
					parts = append(parts, content)
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return agentkit.PromptSection{
		Name:    "memory",
		Content: strings.Join(parts, "\n\n"),
	}, nil
}

func formatMemoryFileContent(data []byte) string {
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return ""
	}
	entries := learning.ParseMemory(raw)
	if len(entries) == 0 {
		return raw
	}
	contents := make([]string, 0, len(entries))
	for _, e := range entries {
		if c := strings.TrimSpace(e.Content); c != "" {
			contents = append(contents, c)
		}
	}
	return strings.Join(contents, "\n\n")
}
