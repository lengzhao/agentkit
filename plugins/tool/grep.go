package tool

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
)

type GrepConfig struct {
	MaxMatches int `json:"maxMatches"`
}

type GrepDeps struct {
	FS filesystem.Service `json:"fs"`
}

type GrepInput struct {
	Pattern    string `json:"pattern" jsonschema:"required,description=Regular expression to search for"`
	Path       string `json:"path" jsonschema:"description=Directory or file to search (default: workspace root)"`
	Glob       string `json:"glob" jsonschema:"description=Optional filename glob filter, e.g. *.go"`
	IgnoreCase bool   `json:"ignoreCase" jsonschema:"description=Case-insensitive search"`
}

type GrepOutput = filesystem.GrepResult

func NewGrep(cfg GrepConfig, deps GrepDeps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/grep requires fs dependency")
	}
	maxMatches := cfg.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 100
	}
	return agentkit.NewTool[GrepInput, GrepOutput]("grep", func(ctx context.Context, input GrepInput) (GrepOutput, error) {
		return deps.FS.Grep(ctx, filesystem.GrepRequest{
			Pattern:    input.Pattern,
			Path:       input.Path,
			Glob:       input.Glob,
			IgnoreCase: input.IgnoreCase,
			MaxMatches: maxMatches,
		})
	}).Description("Search file contents in the workspace using a regular expression.").Build()
}
