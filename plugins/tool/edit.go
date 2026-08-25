package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
)

type EditConfig struct{}

type EditDeps struct {
	FS filesystem.Service `json:"fs"`
}

type FileEdit struct {
	OldText string `json:"oldText" jsonschema:"required,description=Exact text to replace in the original file"`
	NewText string `json:"newText" jsonschema:"required,description=Replacement text"`
}

type EditInput struct {
	Path  string     `json:"path" jsonschema:"required,description=Path to the file to edit"`
	Edits []FileEdit `json:"edits" jsonschema:"required"`
}

type EditOutput struct {
	Path    string `json:"path"`
	Applied bool   `json:"applied"`
}

// NewEditFile registers tool/edit-file: Apply exact search-and-replace edits to an existing file.
//
// Best practices:
//   - oldText must match exactly, including whitespace and indentation.
//   - Batch related edits into one call when they touch the same file.
func NewEditFile(_ EditConfig, deps EditDeps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/edit-file requires fs dependency")
	}
	return agentkit.NewTool("edit", applyEdits(deps.FS)).
		Description("Make precise file edits with exact text replacement. Each oldText is matched against the original file.").Build()
}

func applyEdits(fs filesystem.Service) func(context.Context, EditInput) (EditOutput, error) {
	return func(ctx context.Context, input EditInput) (EditOutput, error) {
		if input.Path == "" {
			return EditOutput{}, fmt.Errorf("path is required")
		}
		if len(input.Edits) == 0 {
			return EditOutput{}, fmt.Errorf("at least one edit is required")
		}
		content, err := fs.ReadText(ctx, input.Path, 0)
		if err != nil {
			return EditOutput{}, err
		}
		updated := content
		for i, edit := range input.Edits {
			if edit.OldText == "" {
				return EditOutput{}, fmt.Errorf("edits[%d].oldText is required", i)
			}
			if !strings.Contains(content, edit.OldText) {
				return EditOutput{}, fmt.Errorf("edits[%d].oldText not found in file", i)
			}
			if strings.Count(content, edit.OldText) > 1 {
				return EditOutput{}, fmt.Errorf("edits[%d].oldText is not unique in file", i)
			}
		}
		applied := false
		for _, edit := range input.Edits {
			if !strings.Contains(updated, edit.OldText) {
				continue
			}
			updated = strings.Replace(updated, edit.OldText, edit.NewText, 1)
			applied = true
		}
		if updated == content {
			return EditOutput{Path: input.Path, Applied: false}, nil
		}
		if err := fs.WriteText(ctx, input.Path, updated); err != nil {
			return EditOutput{}, err
		}
		return EditOutput{Path: input.Path, Applied: applied}, nil
	}
}
