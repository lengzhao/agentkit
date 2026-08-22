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
