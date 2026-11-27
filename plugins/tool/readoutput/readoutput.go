// Package readoutput exposes existing, session-scoped tool output spills.
package readoutput

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}
type Deps struct{}
type Input struct {
	ToolCallID string `json:"tool_call_id" jsonschema:"Tool call ID from the truncated output notice"`
	Offset     int64  `json:"offset,omitempty" jsonschema:"Byte offset; start at 0, then use next_offset from the previous page"`
}

func New(_ Config, _ Deps) (agentkit.Tool, error) {
	return agentkit.NewTool[Input, derive.ToolOutputPage]("read_tool_output", func(ctx context.Context, in Input) (derive.ToolOutputPage, error) {
		return derive.ReadToolOutput(ctx, rctx.SessionIDFromContext(ctx), agentkit.ToolCallID(in.ToolCallID), in.Offset)
	}).Description("Read a truncated tool output from this conversation. Start at offset 0 and follow next_offset until absent. No file paths or other conversations can be accessed.").Build()
}

func init() { pluginkit.Register("tool/read-output", New) }
