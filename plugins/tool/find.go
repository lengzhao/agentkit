package tool

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
)

type FindConfig struct {
	MaxResults int `json:"maxResults"`
}

type FindDeps struct {
	FS filesystem.Service `json:"fs"`
}

type FindInput struct {
	Pattern string `json:"pattern" jsonschema:"required,description=Filename glob pattern, e.g. *.go"`
	Path    string `json:"path" jsonschema:"description=Directory to search (default: workspace root)"`
}

type FindOutput = filesystem.FindResult

func NewFind(cfg FindConfig, deps FindDeps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/find requires fs dependency")
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = 200
	}
	return agentkit.NewTool[FindInput, FindOutput]("find", func(ctx context.Context, input FindInput) (FindOutput, error) {
		return deps.FS.Find(ctx, filesystem.FindRequest{
			Pattern:    input.Pattern,
			Path:       input.Path,
			MaxResults: maxResults,
		})
	}).Description("Find files in the workspace by filename glob pattern (e.g. *.go). Paths are matched relative to the search directory; ** recursive glob is not supported.").Build()
}
