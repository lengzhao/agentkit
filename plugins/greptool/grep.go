package greptool

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	MaxMatches int `json:"maxMatches"`
}

type Deps struct {
	FS filesystem.Service `json:"fs"`
}

type Input struct {
	Pattern    string `json:"pattern" jsonschema:"required,description=Regular expression to search for"`
	Path       string `json:"path" jsonschema:"description=Directory or file to search (default: workspace root)"`
	Glob       string `json:"glob" jsonschema:"description=Optional filename glob filter, e.g. *.go"`
	IgnoreCase bool   `json:"ignoreCase" jsonschema:"description=Case-insensitive search"`
}

type Output = filesystem.GrepResult

func init() {
	pluginkit.Register("tool/grep", New)
}

func New(cfg Config, deps Deps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/grep requires fs dependency")
	}
	maxMatches := cfg.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 100
	}
	return agentkit.NewTool[Input, Output]("grep", func(ctx context.Context, input Input) (Output, error) {
		return deps.FS.Grep(ctx, filesystem.GrepRequest{
			Pattern:    input.Pattern,
			Path:       input.Path,
			Glob:       input.Glob,
			IgnoreCase: input.IgnoreCase,
			MaxMatches: maxMatches,
		})
	}).Description("Search file contents in the workspace using a regular expression.").Build()
}
