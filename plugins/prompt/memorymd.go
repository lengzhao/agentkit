package prompt

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
)

type MemoryMDConfig struct {
	// Root is deprecated (ignored). Use memory.default PromptBody.
	Root string `json:"root"`
	// Filenames is deprecated (ignored).
	Filenames []string `json:"filenames"`
}

type MemoryMDDeps struct {
	Memory capmemory.Reader `json:"memory"`
}

type memoryMDProvider struct {
	memory capmemory.Reader
}

// NewMemoryMD registers prompt/section/memory: inject global + tenant-local memory.md.
func NewMemoryMD(cfg MemoryMDConfig, deps MemoryMDDeps) (agentkit.SectionProvider, error) {
	if deps.Memory == nil {
		return nil, fmt.Errorf("prompt/section/memory requires memory")
	}
	return &memoryMDProvider{memory: deps.Memory}, nil
}

func (p *memoryMDProvider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  "memory",
		Build: p.build,
	}}
}

func (p *memoryMDProvider) build(ctx context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	content, err := loadFrozenMemory(ctx, func() (string, error) {
		return p.memory.PromptBody(ctx)
	})
	if err != nil {
		return agentkit.PromptSection{}, err
	}
	return agentkit.PromptSection{
		Name:    "memory",
		Content: content,
	}, nil
}
