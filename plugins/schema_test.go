package all_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	"github.com/lengzhao/agentkit/plugins/tool/shell"
	"github.com/lengzhao/agentkit/plugins/tool/skill"
)

// schemaOf builds a throwaway tool over In so we exercise the same inference
// path the real plugins use, without needing their fs/skill dependencies.
func schemaOf[In any](t *testing.T) agentkit.JSONSchema {
	t.Helper()
	tool, err := agentkit.NewTool("probe", func(context.Context, In) (struct{}, error) {
		return struct{}{}, nil
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	return tool.InputSchema()
}

// TestToolInputSchemas pins the schema each tool exposes to the model. The
// expected values are the hand-written schemas these plugins carried before
// schema generation moved to inference over the Input struct tags.
func TestToolInputSchemas(t *testing.T) {
	pathProp := func(desc string) agentkit.JSONSchema {
		return agentkit.JSONSchema{Type: "string", Description: desc}
	}

	tests := []struct {
		name string
		got  agentkit.JSONSchema
		want agentkit.JSONSchema
	}{
		{
			name: "find",
			got:  schemaOf[fs.FindInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"pattern": pathProp("Glob pattern to match files, e.g. *.go or **/*.json"),
					"path":    pathProp("Directory to search (default: workspace root)"),
					"limit":   {Type: "integer", Description: "Maximum number of results (default: 1000)"},
				},
				Required: []string{"pattern"},
			},
		},
		{
			name: "grep",
			got:  schemaOf[fs.GrepInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"pattern":    pathProp("Search pattern (regex or literal string)"),
					"path":       pathProp("Directory or file to search (default: workspace root)"),
					"glob":       pathProp("Optional filename glob filter, e.g. *.go"),
					"ignoreCase": {Type: "boolean", Description: "Case-insensitive search"},
					"literal":    {Type: "boolean", Description: "Treat pattern as a literal string instead of a regex"},
					"context":    {Type: "integer", Description: "Lines of context to show before and after each match"},
					"limit":      {Type: "integer", Description: "Maximum number of matches to return (default: 100)"},
				},
				Required: []string{"pattern"},
			},
		},
		{
			name: "ls",
			got:  schemaOf[fs.ListDirInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"path":  pathProp("Directory path relative to the workspace (default: root)"),
					"limit": {Type: "integer", Description: "Maximum number of entries to return (default: 500)"},
				},
			},
		},
		{
			name: "read",
			got:  schemaOf[fs.ReadInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"path":   pathProp("File path relative to the workspace"),
					"offset": {Type: "integer", Description: "Line number to start reading from (1-indexed)"},
					"limit":  {Type: "integer", Description: "Maximum number of lines to read"},
				},
				Required: []string{"path"},
			},
		},
		{
			name: "write",
			got:  schemaOf[fs.WriteInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"path":    pathProp("File path relative to the workspace"),
					"content": pathProp("Full file content to write"),
				},
				Required: []string{"path", "content"},
			},
		},
		{
			name: "bash",
			got:  schemaOf[shell.ShellInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"command": pathProp("Shell command to execute"),
				},
				Required: []string{"command"},
			},
		},
		{
			name: "skill",
			got:  schemaOf[skill.SkillInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"name": pathProp("Skill name to load"),
				},
				Required: []string{"name"},
			},
		},
		{
			name: "edit",
			got:  schemaOf[fs.EditInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"path": pathProp("Path to the file to edit"),
					"edits": {
						Type:        "array",
						Description: "One or more targeted replacements matched against the original file",
						Items: &agentkit.JSONSchema{
							Type: "object",
							Properties: map[string]agentkit.JSONSchema{
								"oldText": pathProp("Exact text to replace in the original file; must be unique and non-overlapping with other edits in the same call"),
								"newText": pathProp("Replacement text"),
							},
							Required: []string{"oldText", "newText"},
						},
					},
				},
				Required: []string{"path", "edits"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := schemaJSON(t, tc.got)
			want := schemaJSON(t, tc.want)
			closeObjects(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("schema mismatch\n got: %s\nwant: %s", mustMarshal(t, got), mustMarshal(t, want))
			}
		})
	}
}

// schemaJSON decodes a schema to the generic form so comparison ignores key
// order, which differs between an inferred schema (a map) and a hand-written
// JSONSchema literal (struct fields).
func schemaJSON(t *testing.T, schema agentkit.JSONSchema) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(mustMarshal(t, schema), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// closeObjects adds the additionalProperties:false that schema inference puts on
// every object. JSONSchema has no field for it, so the expectations above cannot
// spell it out; asserting it here still pins it for every node.
func closeObjects(node map[string]any) {
	if node["type"] == "object" {
		node["additionalProperties"] = false
	}
	if items, ok := node["items"].(map[string]any); ok {
		closeObjects(items)
	}
	if properties, ok := node["properties"].(map[string]any); ok {
		for _, property := range properties {
			if child, ok := property.(map[string]any); ok {
				closeObjects(child)
			}
		}
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
