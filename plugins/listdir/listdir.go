package listdir

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
	Path string `json:"path" jsonschema:"description=Directory path relative to the workspace (default: root)"`
}

type Output struct {
	Entries []filesystem.DirEntry `json:"entries"`
}

func init() {
	pluginkit.Register("tool/list-dir", New)
}

func New(_ Config, deps Deps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/list-dir requires fs dependency")
	}
	return agentkit.NewTool[Input, Output]("ls", func(ctx context.Context, input Input) (Output, error) {
		entries, err := deps.FS.ListDir(ctx, input.Path)
		if err != nil {
			return Output{}, err
		}
		return Output{Entries: entries}, nil
	}).Description("List files and directories in a workspace path.").Build()
}
