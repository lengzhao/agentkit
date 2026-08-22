package agentkit

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
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
			Content: []ContentPart{{
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
		Content: []ContentPart{{
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

// maxSchemaDepth bounds recursion so a self-referential input type cannot hang
// schema generation.
const maxSchemaDepth = 12

// schemaFor derives a JSON Schema from T by reflection. Field names come from
// the `json` tag; an optional `jsonschema` tag supplies `required` and
// `description=...` (the description runs to the end of the tag, so it may
// contain commas). Use ToolBuilder.Schema to override the result entirely.
func schemaFor[T any]() JSONSchema {
	return schemaOfType(reflect.TypeFor[T](), 0)
}

func schemaOfType(t reflect.Type, depth int) JSONSchema {
	if t == nil || depth > maxSchemaDepth {
		return JSONSchema{}
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool:
		return JSONSchema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return JSONSchema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return JSONSchema{Type: "number"}
	case reflect.String:
		return JSONSchema{Type: "string"}
	case reflect.Slice, reflect.Array:
		// []byte marshals to a base64 string, not an array.
		if t.Elem().Kind() == reflect.Uint8 {
			return JSONSchema{Type: "string"}
		}
		item := schemaOfType(t.Elem(), depth+1)
		return JSONSchema{Type: "array", Items: &item}
	case reflect.Map:
		return JSONSchema{Type: "object"}
	case reflect.Struct:
		return structSchema(t, depth)
	default:
		// Interfaces and anything else stay unconstrained.
		return JSONSchema{}
	}
}

func structSchema(t reflect.Type, depth int) JSONSchema {
	out := JSONSchema{Type: "object"}
	properties := make(map[string]JSONSchema)
	var required []string

	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, ok := jsonFieldName(field)
		if !ok {
			continue
		}
		// Embedded struct without an explicit json name is flattened, matching
		// encoding/json.
		if field.Anonymous && name == "" {
			embedded := schemaOfType(field.Type, depth+1)
			for k, v := range embedded.Properties {
				properties[k] = v
			}
			required = append(required, embedded.Required...)
			continue
		}
		schema := schemaOfType(field.Type, depth+1)
		description, isRequired := parseSchemaTag(field.Tag.Get("jsonschema"))
		if description != "" {
			schema.Description = description
		}
		properties[name] = schema
		if isRequired {
			required = append(required, name)
		}
	}

	if len(properties) > 0 {
		out.Properties = properties
	}
	out.Required = required
	return out
}

// jsonFieldName reports the marshalled name of field, and false when the field
// is skipped via `json:"-"`.
func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" && !field.Anonymous {
		name = field.Name
	}
	return name, true
}

func parseSchemaTag(tag string) (description string, required bool) {
	if tag == "" {
		return "", false
	}
	flags := tag
	if i := strings.Index(tag, "description="); i >= 0 {
		description = tag[i+len("description="):]
		flags = tag[:i]
	}
	for _, flag := range strings.Split(flags, ",") {
		if strings.TrimSpace(flag) == "required" {
			required = true
		}
	}
	return description, required
}
