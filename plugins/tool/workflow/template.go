package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/template"
)

// templateFuncMap is deliberately tiny: no file or network access, so
// model-influenced inputs cannot reach beyond the pipeline's own data.
var templateFuncMap = template.FuncMap{
	"dig":   tmplDig,
	"quote": tmplQuote,
	"join":  tmplJoin,
}

// tmplDig walks nested maps/slices by dotted path: dig .steps.submit.json "status".
func tmplDig(value any, path string) (any, error) {
	cur := value
	for _, key := range strings.Split(path, ".") {
		if wrapped, ok := cur.(JSON); ok {
			cur = wrapped.V
		}
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[key]
			if !ok {
				return nil, fmt.Errorf("dig %q: no key %q", path, key)
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(node) {
				return nil, fmt.Errorf("dig %q: bad index %q", path, key)
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("dig %q: %q is not an object or array", path, key)
		}
	}
	// Composite results render as compact JSON in direct template actions.
	switch cur.(type) {
	case map[string]any, []any:
		return JSON{V: cur}, nil
	}
	return cur, nil
}

// tmplQuote shell-quotes a value for safe interpolation into script commands.
func tmplQuote(value any) (string, error) {
	s, err := stringify(value)
	if err != nil {
		return "", err
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", nil
}

// tmplJoin renders a slice as a separator-joined string.
func tmplJoin(value any, sep string) (string, error) {
	if wrapped, ok := value.(JSON); ok {
		value = wrapped.V
	}
	list, ok := value.([]any)
	if !ok {
		return "", fmt.Errorf("join: value is not an array")
	}
	parts := make([]string, 0, len(list))
	for _, item := range list {
		s, err := stringify(item)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, sep), nil
}

// stringify renders scalars as-is and composite values as compact JSON, so
// {{ .steps.convert.json }} embeds valid JSON into downstream args.
func stringify(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case JSON:
		return stringify(v.V)
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("render value: %w", err)
		}
		return string(raw), nil
	}
}

func renderTemplate(name, text string, data map[string]any) (string, error) {
	tmpl, err := template.New(name).Funcs(templateFuncMap).Parse(text)
	if err != nil {
		return "", fmt.Errorf("template %s: %w", name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("template %s: %w", name, err)
	}
	return out.String(), nil
}

// renderArgs deep-renders template strings in tool-step args.
func renderArgs(args map[string]any, data map[string]any) (map[string]any, error) {
	if len(args) == 0 {
		return map[string]any{}, nil
	}
	rendered, err := renderValue(args, data)
	if err != nil {
		return nil, err
	}
	out, ok := rendered.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool args did not render to an object")
	}
	return out, nil
}

func renderValue(value any, data map[string]any) (any, error) {
	switch v := value.(type) {
	case string:
		return renderTemplate("args", v, data)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			rendered, err := renderValue(item, data)
			if err != nil {
				return nil, fmt.Errorf("args.%s: %w", key, err)
			}
			out[key] = rendered
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			rendered, err := renderValue(item, data)
			if err != nil {
				return nil, fmt.Errorf("args[%d]: %w", i, err)
			}
			out[i] = rendered
		}
		return out, nil
	default:
		return value, nil
	}
}
