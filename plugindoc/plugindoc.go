// Package plugindoc holds the documentation shown by `/help plugin`: a
// kind -> Doc registry plus a renderer.
//
// It deliberately depends on nothing but pluginkit and the standard library.
// Half of the plugin packages provide cap/* interfaces and never import the root
// agentkit package; if Doc lived there, documenting fs/local or shell/bash would
// drag the harness core into those packages for nothing.
//
// The other half of a kind's documentation is not written by hand at all: field
// names, types and nesting come from the constructor's Config and Deps types via
// pluginkit.Spec. A Doc supplies only what reflection cannot know.
package plugindoc

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/lengzhao/pluginkit"
)

// Doc is the hand-written half of a plugin kind's documentation. It must not
// restate the config field list — that is generated. Keeping field names in
// exactly one place (the struct) is what stops these docs from drifting.
type Doc struct {
	// Summary is one line: what this plugin does.
	Summary string `json:"summary,omitempty"`
	// ConfigNotes annotates individual config fields, keyed by json name.
	// TestConfigNotesMatchSchema rejects a key that no config field has, so a
	// renamed field fails the build instead of silently documenting a dead key.
	ConfigNotes map[string]string `json:"configNotes,omitempty"`
	// BestPractices are short usage tips, rendered as a bullet list.
	BestPractices []string `json:"bestPractices,omitempty"`
}

var docs = map[string]Doc{}

// Register associates documentation with a plugin kind. Call it from the same
// package as the plugin, so the notes sit next to the Config struct they
// describe.
func Register(kind string, doc Doc) {
	if kind == "" {
		panic("plugindoc.Register: kind must not be empty")
	}
	if _, exists := docs[kind]; exists {
		panic("plugindoc.Register: duplicate doc for kind " + kind)
	}
	docs[kind] = doc
}

// Lookup returns the doc registered for kind.
func Lookup(kind string) (Doc, bool) {
	doc, ok := docs[kind]
	return doc, ok
}

// Documented returns the kinds that have a registered doc, sorted.
func Documented() []string {
	out := make([]string, 0, len(docs))
	for kind := range docs {
		out = append(out, kind)
	}
	slices.Sort(out)
	return out
}

// Kinds returns every registered pluginkit kind, sorted.
func Kinds() []string { return pluginkit.ListKinds() }

// FormatList renders every registered kind with its one-line summary.
func FormatList() string {
	kinds := Kinds()
	width := 0
	for _, kind := range kinds {
		width = max(width, len(kind))
	}
	var b strings.Builder
	b.WriteString("Registered plugin kinds:\n")
	for _, kind := range kinds {
		doc, _ := Lookup(kind)
		summary := doc.Summary
		if summary == "" {
			summary = "(undocumented)"
		}
		fmt.Fprintf(&b, "  %-*s  %s\n", width, kind, summary)
	}
	b.WriteString("\nUse /help plugin <kind> for config and dependency details.")
	return b.String()
}

// FormatKind renders one kind: the hand-written summary and tips, plus the
// config and dependency fields read off the constructor's signature.
func FormatKind(kind string) (string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "", fmt.Errorf("plugin kind is required")
	}
	spec, ok := pluginkit.Lookup(kind)
	if !ok {
		return "", fmt.Errorf("unknown plugin kind %q (try /help plugin -l)", kind)
	}
	doc, _ := Lookup(kind)

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", kind)
	if doc.Summary != "" {
		fmt.Fprintf(&b, "\n%s\n", doc.Summary)
	}
	if fields := FormatFields(spec.ConfigType, doc.ConfigNotes); fields != "" {
		fmt.Fprintf(&b, "\n## Config\n\n%s\n", fields)
	}
	if fields := FormatFields(spec.DepsType, nil); fields != "" {
		fmt.Fprintf(&b, "\n## Deps\n\n%s\n", fields)
	}
	if len(doc.BestPractices) > 0 {
		b.WriteString("\n## Best practices\n\n")
		for _, tip := range doc.BestPractices {
			fmt.Fprintf(&b, "- %s\n", tip)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// maxFieldDepth bounds nested struct rendering. Real config trees go two levels
// deep (agent retry/budget, llm reasoning); the bound guards against a type that
// refers back to itself.
const maxFieldDepth = 3

// FormatFields renders a Config or Deps struct as an indented field list, with
// notes annotating top-level fields by json name. A nil type renders empty.
func FormatFields(t reflect.Type, notes map[string]string) string {
	var b strings.Builder
	writeFields(&b, t, notes, "", 0)
	return strings.TrimRight(b.String(), "\n")
}

// ConfigFieldNames returns the json names of t's fields, top level only. It is
// what validates Doc.ConfigNotes keys.
func ConfigFieldNames(t reflect.Type) []string {
	t = deref(t)
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, ok := jsonName(field)
		if !ok {
			continue
		}
		if name == "" {
			out = append(out, ConfigFieldNames(field.Type)...)
			continue
		}
		out = append(out, name)
	}
	return out
}

func writeFields(b *strings.Builder, t reflect.Type, notes map[string]string, indent string, depth int) {
	t = deref(t)
	if t == nil || t.Kind() != reflect.Struct || depth >= maxFieldDepth {
		return
	}
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, ok := jsonName(field)
		if !ok {
			continue
		}
		if name == "" {
			// Embedded struct with no json name: its fields marshal inline, so
			// render them at this level rather than hiding them.
			writeFields(b, field.Type, notes, indent, depth)
			continue
		}
		fmt.Fprintf(b, "%s- %s (%s)", indent, name, field.Type)
		if note := notes[name]; note != "" {
			fmt.Fprintf(b, ": %s", note)
		}
		b.WriteByte('\n')
		// Nested config objects are part of the YAML an author has to write, so
		// expand them. Interfaces (deps) and stdlib types are leaves.
		if inner := deref(elem(field.Type)); inner != nil && inner.Kind() == reflect.Struct && isProjectType(inner) {
			writeFields(b, inner, nil, indent+"  ", depth+1)
		}
	}
}

// jsonName reports the marshalled name of field, and false when the field is
// skipped via `json:"-"`. An empty name with ok means an inlined embedded field.
func jsonName(field reflect.StructField) (string, bool) {
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

func deref(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func elem(t reflect.Type) reflect.Type {
	for t != nil {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			t = t.Elem()
		default:
			return t
		}
	}
	return nil
}

// isProjectType reports whether t is one of this project's own structs, i.e.
// worth expanding. It keeps time.Duration, json.RawMessage and friends as leaves.
func isProjectType(t reflect.Type) bool {
	return strings.HasPrefix(t.PkgPath(), "github.com/lengzhao/agentkit")
}
