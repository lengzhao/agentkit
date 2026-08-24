package tool

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
)

type WriteConfig struct{}

type WriteDeps struct {
	FS filesystem.Service `json:"fs"`
}

type WriteInput struct {
	Path    string `json:"path" jsonschema:"required,description=File path relative to the workspace"`
	Content string `json:"content" jsonschema:"required,description=Full file content to write"`
}

type WriteOutput struct {
	Path string `json:"path"`
}

func NewWriteFile(_ WriteConfig, deps WriteDeps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/write-file requires fs dependency")
	}
	return agentkit.NewTool[WriteInput, WriteOutput]("write", func(ctx context.Context, input WriteInput) (WriteOutput, error) {
		if err := deps.FS.WriteText(ctx, input.Path, input.Content); err != nil {
			return WriteOutput{}, err
		}
		return WriteOutput{Path: input.Path}, nil
	}).Description("Write content to a file in the workspace.").Build()
}
