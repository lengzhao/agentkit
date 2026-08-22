package findtool

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	MaxResults int `json:"maxResults"`
}

type Deps struct {
	FS filesystem.Service `json:"fs"`
}

type Input struct {
	Pattern string `json:"pattern" jsonschema:"required,description=Filename glob pattern, e.g. *.go"`
	Path    string `json:"path" jsonschema:"description=Directory to search (default: workspace root)"`
}

type Output = filesystem.FindResult

func init() {
	pluginkit.Register("tool/find", New)
}

func New(cfg Config, deps Deps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/find requires fs dependency")
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = 200
	}
	return agentkit.NewTool[Input, Output]("find", func(ctx context.Context, input Input) (Output, error) {
		return deps.FS.Find(ctx, filesystem.FindRequest{
			Pattern:    input.Pattern,
			Path:       input.Path,
			MaxResults: maxResults,
		})
	}).Description("Find files in the workspace by filename glob pattern (e.g. *.go). Paths are matched relative to the search directory; ** recursive glob is not supported.").Build()
}
