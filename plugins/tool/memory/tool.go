package memory

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
	"github.com/lengzhao/pluginkit"
)

type MemoryToolDeps struct {
	Memory capmemory.Tool `json:"memory"`
}

func init() {
	pluginkit.Register("tool/memory", NewMemoryTool)
}

// NewMemoryTool registers tool/memory for the main agent.
func NewMemoryTool(_ struct{}, deps MemoryToolDeps) (agentkit.Tool, error) {
	if deps.Memory == nil {
		return nil, fmt.Errorf("tool/memory requires memory")
	}
	svc := deps.Memory
	return agentkit.NewTool[capmemory.MemoryToolInput, capmemory.MemoryToolOutput]("memory", func(ctx context.Context, input capmemory.MemoryToolInput) (capmemory.MemoryToolOutput, error) {
		return svc.MemoryTool(ctx, input)
	}).
		Description(rtmem.MemoryToolDescription).
		Build()
}
