package readfile

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	MaxBytes int `json:"maxBytes"`
}

type Deps struct {
	FS filesystem.Service `json:"fs"`
}

type Input struct {
	Path string `json:"path"`
}

type Output struct {
	Content string `json:"content"`
}

func init() {
	pluginkit.Register("tool/read-file", New)
}

func New(cfg Config, deps Deps) (agentkit.Tool, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/read-file requires fs dependency")
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	return agentkit.NewTool[Input, Output]("read", func(ctx context.Context, input Input) (Output, error) {
		content, err := deps.FS.ReadText(ctx, input.Path, maxBytes)
		if err != nil {
			return Output{}, err
		}
		return Output{Content: content}, nil
	}).Description("Read a text file from the workspace.").Schema(agentkit.JSONSchema{
		Type: "object",
		Properties: map[string]agentkit.JSONSchema{
			"path": {Type: "string", Description: "File path relative to the workspace"},
		},
		Required: []string{"path"},
	}).Build()
}

func MustRaw(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
