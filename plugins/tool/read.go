package tool

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
)

type ReadConfig struct {
	// MaxBytes is truncation limit; defaults to 1 MiB.
	MaxBytes int `json:"maxBytes"`
}

type ReadDeps struct {
	FS filesystem.Service `json:"fs"`
}

type ReadInput struct {
	Path string `json:"path" jsonschema:"required,description=File path relative to the workspace"`
}

type ReadOutput struct {
	Content string `json:"content"`
}

// NewReadFile registers tool/read-file: Read a text file from the workspace.
//
// Best practices:
//   - Prefer grep for searching inside files instead of reading whole trees.
func NewReadFile(cfg ReadConfig, deps ReadDeps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/read-file requires fs dependency")
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	return agentkit.NewTool[ReadInput, ReadOutput]("read", func(ctx context.Context, input ReadInput) (ReadOutput, error) {
		content, err := deps.FS.ReadText(ctx, input.Path, maxBytes)
		if err != nil {
			return ReadOutput{}, err
		}
		return ReadOutput{Content: content}, nil
	}).Description("Read a text file from the workspace.").Build()
}
