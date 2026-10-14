package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// templateFuncs is deliberately tiny. readJSON is the only file access, and
// it is limited to the workflow file directory and the current run directory
// so model-influenced paths cannot read arbitrary files.
func templateFuncs(data map[string]any) template.FuncMap {
	return template.FuncMap{
		"dig":      tmplDig,
		"quote":    tmplQuote,
		"join":     tmplJoin,
		"readJSON": func(path string) (any, error) { return tmplReadJSON(data, path) },
	}
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
		if n, ok := floatToNumber(v); ok {
			return n.String(), nil
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case json.Number:
		return v.String(), nil
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
	tmpl, err := template.New(name).Funcs(templateFuncs(data)).Parse(text)
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
		rendered, err := renderTemplate("args", v, data)
		if err != nil {
			return nil, err
		}
		// A rendered leaf that is a JSON object or array is the tool argument
		// itself. Parsing it here avoids a second encoding that would turn
		// the object into a JSON string.
		if promoted, ok, err := promoteJSONComposite(rendered); ok || err != nil {
			return promoted, err
		}
		// A leaf that is only one interpolation and renders as a JSON number
		// stays a number (expectedVersion and similar integer fields). A
		// static string, even "42", is left as a string.
		if n, ok := promoteJSONNumber(v, rendered); ok {
			return n, nil
		}
		return rendered, nil
	case float64:
		if n, ok := floatToNumber(v); ok {
			return n, nil
		}
		return v, nil
	case json.Number:
		return v, nil
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

// decodeJSON decodes one JSON value and keeps numbers as json.Number.
// encoding/json otherwise stores every number in any as float64, which
// rounds integers above 2^53 and makes integer-typed tool fields ambiguous.
func decodeJSON(text string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("extra JSON value")
		}
		return nil, err
	}
	return value, nil
}

// promoteJSONComposite parses a rendered arg when the whole value is a JSON
// object or array. Other strings, including numeric text, stay strings.
func promoteJSONComposite(rendered string) (any, bool, error) {
	trimmed := strings.TrimSpace(rendered)
	if !jsonComposite(trimmed) {
		return nil, false, nil
	}
	value, err := decodeJSON(trimmed)
	if err != nil {
		return nil, false, fmt.Errorf("JSON object or array: %w", err)
	}
	switch value.(type) {
	case map[string]any, []any:
		return value, true, nil
	default:
		return nil, false, nil
	}
}

// promoteJSONNumber keeps an interpolated JSON number as json.Number so
// integer tool fields are not sent as strings.
func promoteJSONNumber(templateText, rendered string) (json.Number, bool) {
	if !singleTemplateAction(templateText) {
		return "", false
	}
	value, err := decodeJSON(strings.TrimSpace(rendered))
	if err != nil {
		return "", false
	}
	n, ok := value.(json.Number)
	return n, ok
}

func singleTemplateAction(text string) bool {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{{") || !strings.HasSuffix(trimmed, "}}") {
		return false
	}
	inner := trimmed[2 : len(trimmed)-2]
	return !strings.Contains(inner, "{{") && !strings.Contains(inner, "}}")
}

func jsonComposite(s string) bool {
	if len(s) < 2 {
		return false
	}
	return (s[0] == '{' && s[len(s)-1] == '}') || (s[0] == '[' && s[len(s)-1] == ']')
}

// floatToNumber rewrites a float64 that came from encoding/json into the
// original integer literal when the value is a whole number in int64 range.
func floatToNumber(n float64) (json.Number, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return "", false
	}
	if n == math.Trunc(n) && n >= math.MinInt64 && n <= math.MaxInt64 {
		return json.Number(strconv.FormatInt(int64(n), 10)), true
	}
	return json.Number(strconv.FormatFloat(n, 'f', -1, 64)), true
}

// tmplReadJSON reads a JSON object or array from the workflow directory or
// the current run directory and returns it for injection into a tool arg.
func tmplReadJSON(data map[string]any, path string) (any, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("readJSON: empty path")
	}
	roots := workflowRoots(data)
	if len(roots) == 0 {
		return nil, fmt.Errorf("readJSON: workflow directory is not available")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("readJSON: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("readJSON %q: %w", path, err)
	}
	resolved = filepath.Clean(resolved)
	if !withinRoots(roots, resolved) {
		return nil, fmt.Errorf("readJSON: %q is outside the workflow directory", path)
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("readJSON %q: %w", path, err)
	}
	value, err := decodeJSON(string(raw))
	if err != nil {
		return nil, fmt.Errorf("readJSON %q: %w", path, err)
	}
	switch value.(type) {
	case map[string]any, []any:
		return JSON{V: value}, nil
	default:
		return nil, fmt.Errorf("readJSON %q: JSON value must be an object or array", path)
	}
}

func workflowRoots(data map[string]any) []string {
	wf, _ := data["workflow"].(map[string]any)
	if wf == nil {
		return nil
	}
	var roots []string
	for _, key := range []string{"runDir", "dir"} {
		s, _ := wf[key].(string)
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		abs, err := filepath.Abs(s)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		roots = append(roots, filepath.Clean(abs))
	}
	return roots
}

func withinRoots(roots []string, path string) bool {
	for _, root := range roots {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}
