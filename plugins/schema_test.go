package all_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tool"
)

// schemaOf builds a throwaway tool over In so we exercise the same reflection
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
// schema generation moved to reflection over the Input struct tags.
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
			got:  schemaOf[tool.FindInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"pattern": pathProp("Filename glob pattern, e.g. *.go"),
					"path":    pathProp("Directory to search (default: workspace root)"),
				},
				Required: []string{"pattern"},
			},
		},
		{
			name: "grep",
			got:  schemaOf[tool.GrepInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"pattern":    pathProp("Regular expression to search for"),
					"path":       pathProp("Directory or file to search (default: workspace root)"),
					"glob":       pathProp("Optional filename glob filter, e.g. *.go"),
					"ignoreCase": {Type: "boolean", Description: "Case-insensitive search"},
				},
				Required: []string{"pattern"},
			},
		},
		{
			name: "ls",
			got:  schemaOf[tool.ListDirInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"path": pathProp("Directory path relative to the workspace (default: root)"),
				},
			},
		},
		{
			name: "read",
			got:  schemaOf[tool.ReadInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"path": pathProp("File path relative to the workspace"),
				},
				Required: []string{"path"},
			},
		},
		{
			name: "write",
			got:  schemaOf[tool.WriteInput](t),
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
			got:  schemaOf[tool.ShellInput](t),
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
			got:  schemaOf[tool.SkillInput](t),
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
			got:  schemaOf[tool.EditInput](t),
			want: agentkit.JSONSchema{
				Type: "object",
				Properties: map[string]agentkit.JSONSchema{
					"path": pathProp("Path to the file to edit"),
					"edits": {
						Type: "array",
						Items: &agentkit.JSONSchema{
							Type: "object",
							Properties: map[string]agentkit.JSONSchema{
								"oldText": pathProp("Exact text to replace in the original file"),
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
			got, err := json.Marshal(tc.got)
			if err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("schema mismatch\n got: %s\nwant: %s", got, want)
			}
		})
	}
}
