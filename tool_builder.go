package agentkit

import (
	"context"
	"encoding/json"
	"fmt"
)

type toolImpl[In, Out any] struct {
	name        string
	description string
	schema      JSONSchema
	handler     func(context.Context, In) (Out, error)
}

func (t *toolImpl[In, Out]) Name() string        { return t.name }
func (t *toolImpl[In, Out]) Description() string { return t.description }
func (t *toolImpl[In, Out]) InputSchema() JSONSchema {
	return t.schema
}

func (t *toolImpl[In, Out]) Call(ctx context.Context, call ToolCall) (ToolResult, error) {
	var input In
	if len(call.Input) == 0 {
		call.Input = json.RawMessage("{}")
	}
	raw := call.Input
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			raw = json.RawMessage(s)
		}
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return ToolResult{}, fmt.Errorf("invalid tool input: %w", err)
	}
	out, err := t.handler(ctx, input)
	if err != nil {
		return ToolResult{
			ID:   call.ID,
			Name: call.Name,
			Content: []ToolContent{{
				Type: "text",
				Text: err.Error(),
			}},
		}, nil
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{
		ID:   call.ID,
		Name: call.Name,
		Content: []ToolContent{{
			Type: "text",
			Text: string(encoded),
		}},
	}, nil
}

type ToolBuilder[In, Out any] struct {
	name        string
	description string
	schema      JSONSchema
	handler     func(context.Context, In) (Out, error)
}

// NewTool starts building a typed tool plugin.
func NewTool[In, Out any](name string, handler func(context.Context, In) (Out, error)) *ToolBuilder[In, Out] {
	return &ToolBuilder[In, Out]{
		name:    name,
		handler: handler,
		schema:  schemaFor[In](),
	}
}

func (b *ToolBuilder[In, Out]) Description(desc string) *ToolBuilder[In, Out] {
	b.description = desc
	return b
}

func (b *ToolBuilder[In, Out]) Schema(schema JSONSchema) *ToolBuilder[In, Out] {
	b.schema = schema
	return b
}

func (b *ToolBuilder[In, Out]) Build() (Tool, error) {
	if b.name == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	if b.handler == nil {
		return nil, fmt.Errorf("tool handler is required")
	}
	return &toolImpl[In, Out]{
		name:        b.name,
		description: b.description,
		schema:      b.schema,
		handler:     b.handler,
	}, nil
}

func schemaFor[T any]() JSONSchema {
	switch any(*new(T)).(type) {
	case struct {
		Path string `json:"path" jsonschema:"required,description=File path relative to the workspace"`
	}:
		return JSONSchema{
			Type: "object",
			Properties: map[string]JSONSchema{
				"path": {Type: "string", Description: "File path relative to the workspace"},
			},
			Required: []string{"path"},
		}
	case struct {
		Path    string `json:"path" jsonschema:"required"`
		Content string `json:"content" jsonschema:"required"`
	}:
		return JSONSchema{
			Type: "object",
			Properties: map[string]JSONSchema{
				"path":    {Type: "string", Description: "File path relative to the workspace"},
				"content": {Type: "string", Description: "Full file content to write"},
			},
			Required: []string{"path", "content"},
		}
	case struct {
		Command string `json:"command" jsonschema:"required"`
	}:
		return JSONSchema{
			Type: "object",
			Properties: map[string]JSONSchema{
				"command": {Type: "string", Description: "Shell command to execute"},
			},
			Required: []string{"command"},
		}
	default:
		return JSONSchema{Type: "object"}
	}
}
