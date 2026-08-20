package shelltool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/shell"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	Shell shell.Executor `json:"shell"`
}

type Input struct {
	Command string `json:"command"`
}

type Output struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func init() {
	pluginkit.Register("tool/shell", New)
}

func New(_ Config, deps Deps) (agentkit.Tool, error) {
	if deps.Shell == nil {
		return nil, fmt.Errorf("tool/shell requires shell dependency")
	}
	return agentkit.NewTool("bash", func(ctx context.Context, input Input) (Output, error) {
		result, err := deps.Shell.Run(ctx, shell.Request{Command: input.Command})
		if err != nil {
			return Output{}, err
		}
		return Output{
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, nil
	}).Description("Execute a bash command in the workspace.").Schema(agentkit.JSONSchema{
		Type: "object",
		Properties: map[string]agentkit.JSONSchema{
			"command": {Type: "string", Description: "Shell command to execute"},
		},
		Required: []string{"command"},
	}).Build()
}

func ExampleOutput() json.RawMessage {
	raw, _ := json.Marshal(Output{ExitCode: 0, Stdout: "ok"})
	return raw
}
