package agentkit

import (
	"context"
	"encoding/json"
)

// Tool is the model-visible consumer plugin type.
type Tool interface {
	Name() string
	Description() string
	InputSchema() JSONSchema
	Call(context.Context, ToolCall) (ToolResult, error)
}

// ToolPack is one plugin instance that exposes one or more model-visible tools.
type ToolPack []Tool

// Pack returns a ToolPack from individual tools.
func Pack(tools ...Tool) ToolPack {
	return tools
}

// First returns the only tool in a single-tool pack.
func First(pack ToolPack) Tool {
	if len(pack) == 0 {
		return nil
	}
	return pack[0]
}

type ToolSpec struct {
	Name        string
	Description string
	InputSchema JSONSchema
}

type ToolCall struct {
	ID    ToolCallID
	Name  string
	Input json.RawMessage
}

type ToolResult struct {
	ID      ToolCallID
	Name    string
	Content []ContentPart
	Audit   map[string]string
}

type ToolRuntime interface {
	Visible(context.Context) ([]ToolSpec, error)
	Execute(context.Context, ToolCall) (ToolResult, error)
}
