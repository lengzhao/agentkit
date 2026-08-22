package writefile

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	FS filesystem.Service `json:"fs"`
}

type Input struct {
	Path    string `json:"path" jsonschema:"required,description=File path relative to the workspace"`
	Content string `json:"content" jsonschema:"required,description=Full file content to write"`
}

type Output struct {
	Path string `json:"path"`
}

func init() {
	pluginkit.Register("tool/write-file", New)
}

func New(_ Config, deps Deps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/write-file requires fs dependency")
	}
	return agentkit.NewTool[Input, Output]("write", func(ctx context.Context, input Input) (Output, error) {
		if err := deps.FS.WriteText(ctx, input.Path, input.Content); err != nil {
			return Output{}, err
		}
		return Output{Path: input.Path}, nil
	}).Description("Write content to a file in the workspace.").Build()
}
