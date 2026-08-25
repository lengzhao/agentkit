package tool

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
)

type ListDirConfig struct{}

type ListDirDeps struct {
	FS filesystem.Service `json:"fs"`
}

type ListDirInput struct {
	Path string `json:"path" jsonschema:"description=Directory path relative to the workspace (default: root)"`
}

type ListDirOutput struct {
	Entries []filesystem.DirEntry `json:"entries"`
}

// NewListDir registers tool/list-dir: List entries in a workspace directory.
func NewListDir(_ ListDirConfig, deps ListDirDeps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/list-dir requires fs dependency")
	}
	return agentkit.NewTool[ListDirInput, ListDirOutput]("ls", func(ctx context.Context, input ListDirInput) (ListDirOutput, error) {
		entries, err := deps.FS.ListDir(ctx, input.Path)
		if err != nil {
			return ListDirOutput{}, err
		}
		return ListDirOutput{Entries: entries}, nil
	}).Description("List files and directories in a workspace path.").Build()
}
