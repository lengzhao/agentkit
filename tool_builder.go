package agentkit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
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

func (t *toolImpl[In, Out]) Call(ctx context.Context, input json.RawMessage) (string, error) {
	var decoded In
	raw := input
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	// Some models send the arguments object as a JSON string; unwrap one level.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			raw = json.RawMessage(s)
		}
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("invalid tool input: %w", err)
	}
	out, err := t.handler(ctx, decoded)
	if err != nil {
		return err.Error(), nil
	}
	return FormatToolResult(out)
}

type ToolBuilder[In, Out any] struct {
	name        string
	description string
	schema      JSONSchema
	schemaErr   error
	handler     func(context.Context, In) (Out, error)
}

// NewTool starts building a typed tool plugin. The input schema is inferred
// from In; any inference error surfaces from Build. Handler output is adapted
// to model-visible text: string is returned as-is, other values are JSON.
func NewTool[In, Out any](name string, handler func(context.Context, In) (Out, error)) *ToolBuilder[In, Out] {
	b := &ToolBuilder[In, Out]{
		name:    name,
		handler: handler,
	}
	b.schema, b.schemaErr = schemaFor[In]()
	return b
}

func (b *ToolBuilder[In, Out]) Description(desc string) *ToolBuilder[In, Out] {
	b.description = desc
	return b
}

// Schema replaces the inferred input schema, discarding any inference error.
func (b *ToolBuilder[In, Out]) Schema(schema JSONSchema) *ToolBuilder[In, Out] {
	b.schema = schema
	b.schemaErr = nil
	return b
}

func (b *ToolBuilder[In, Out]) Build() (Tool, error) {
	if b.name == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	if b.handler == nil {
		return nil, fmt.Errorf("tool handler is required")
	}
	if b.schemaErr != nil {
		return nil, fmt.Errorf("tool %s input schema: %w", b.name, b.schemaErr)
	}
	return &toolImpl[In, Out]{
		name:        b.name,
		description: b.description,
		schema:      b.schema,
		handler:     b.handler,
	}, nil
}

// FormatToolResult converts a typed tool handler result into model-visible text.
func FormatToolResult(out any) (string, error) {
	if text, ok := out.(string); ok {
		return text, nil
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// schemaFor derives a JSON Schema from T with jsonschema-go. Field names come
// from the `json` tag; a `jsonschema` tag is the property description in full;
// a field is required unless its json tag carries omitempty or omitzero. Use
// ToolBuilder.Schema to override the result entirely.
func schemaFor[T any]() (JSONSchema, error) {
	inferred, err := jsonschema.For[T](nil)
	if err != nil {
		return JSONSchema{}, err
	}
	raw, err := json.Marshal(inferred)
	if err != nil {
		return JSONSchema{}, err
	}
	var node map[string]any
	if err := json.Unmarshal(raw, &node); err != nil {
		return JSONSchema{}, err
	}
	normalizeSchemaNode(node)
	normalized, err := json.Marshal(node)
	if err != nil {
		return JSONSchema{}, err
	}
	var out JSONSchema
	if err := json.Unmarshal(normalized, &out); err != nil {
		return JSONSchema{}, err
	}
	return out, nil
}

// normalizeSchemaNode rewrites inferred schema JSON into the shape model
// providers expect. Go's nilable types infer as a ["null", T] type union, which
// only describes how Go spells absence, and jsonschema-go writes a closed
// object as {"not":{}} rather than the literal false that provider strict modes
// look for.
func normalizeSchemaNode(node map[string]any) {
	if types, ok := node["type"].([]any); ok {
		kept := make([]any, 0, len(types))
		for _, t := range types {
			if t != "null" {
				kept = append(kept, t)
			}
		}
		switch len(kept) {
		case 0:
			delete(node, "type")
		case 1:
			node["type"] = kept[0]
		default:
			node["type"] = kept
		}
	}
	if extra, ok := node["additionalProperties"].(map[string]any); ok {
		if not, ok := extra["not"].(map[string]any); ok && len(not) == 0 && len(extra) == 1 {
			node["additionalProperties"] = false
		} else {
			normalizeSchemaNode(extra)
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		normalizeSchemaNode(items)
	}
	if properties, ok := node["properties"].(map[string]any); ok {
		for _, property := range properties {
			if child, ok := property.(map[string]any); ok {
				normalizeSchemaNode(child)
			}
		}
	}
}
