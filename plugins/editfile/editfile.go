package editfile

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	FS filesystem.Service `json:"fs"`
}

type Edit struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

type Input struct {
	Path  string `json:"path"`
	Edits []Edit `json:"edits"`
}

type Output struct {
	Path    string `json:"path"`
	Applied bool   `json:"applied"`
}

func init() {
	pluginkit.Register("tool/edit-file", New)
}

func New(_ Config, deps Deps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/edit-file requires fs dependency")
	}
	return agentkit.NewTool("edit", applyEdits(deps.FS)).
		Description("Make precise file edits with exact text replacement. Each oldText is matched against the original file.").
		Schema(agentkit.JSONSchema{
			Type: "object",
			Properties: map[string]agentkit.JSONSchema{
				"path": {Type: "string", Description: "Path to the file to edit"},
				"edits": {
					Type: "array",
					Items: &agentkit.JSONSchema{
						Type: "object",
						Properties: map[string]agentkit.JSONSchema{
							"oldText": {Type: "string", Description: "Exact text to replace in the original file"},
							"newText": {Type: "string", Description: "Replacement text"},
						},
						Required: []string{"oldText", "newText"},
					},
				},
			},
			Required: []string{"path", "edits"},
		}).Build()
}

func applyEdits(fs filesystem.Service) func(context.Context, Input) (Output, error) {
	return func(ctx context.Context, input Input) (Output, error) {
		if input.Path == "" {
			return Output{}, fmt.Errorf("path is required")
		}
		if len(input.Edits) == 0 {
			return Output{}, fmt.Errorf("at least one edit is required")
		}
		content, err := fs.ReadText(ctx, input.Path, 0)
		if err != nil {
			return Output{}, err
		}
		updated := content
		for i, edit := range input.Edits {
			if edit.OldText == "" {
				return Output{}, fmt.Errorf("edits[%d].oldText is required", i)
			}
			if !strings.Contains(content, edit.OldText) {
				return Output{}, fmt.Errorf("edits[%d].oldText not found in file", i)
			}
			if strings.Count(content, edit.OldText) > 1 {
				return Output{}, fmt.Errorf("edits[%d].oldText is not unique in file", i)
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
			return Output{Path: input.Path, Applied: false}, nil
		}
		if err := fs.WriteText(ctx, input.Path, updated); err != nil {
			return Output{}, err
		}
		return Output{Path: input.Path, Applied: applied}, nil
	}
}
