package tools

import "github.com/lengzhao/agentkit"

// defaultSequentialToolNames are parallel-unsafe until plugins set ExecutionMode explicitly.
var defaultSequentialToolNames = map[string]bool{
	"bash":      true,
	"edit":      true,
	"write":     true,
	"memory":    true,
	"skill":     true,
	"send":      true,
	"tool_call": true,
}

func toolExecutionMode(tool agentkit.Tool) agentkit.ToolExecutionMode {
	if p, ok := tool.(agentkit.ToolExecutionModeProvider); ok {
		mode := p.ExecutionMode()
		if mode != "" {
			return mode
		}
	}
	if defaultSequentialToolNames[tool.Name()] || defaultSequentialToolNames[ExposedToolName(tool.Name())] {
		return agentkit.ToolExecutionSequential
	}
	return agentkit.ToolExecutionParallel
}

func specExecutionMode(tool agentkit.Tool) agentkit.ToolExecutionMode {
	return toolExecutionMode(tool)
}
