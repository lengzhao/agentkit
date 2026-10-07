package agentkit

import "context"

// ToolExecutionMode controls how tool calls from one assistant message run together.
type ToolExecutionMode string

const (
	// ToolExecutionParallel runs independent tools concurrently (default).
	ToolExecutionParallel ToolExecutionMode = "parallel"
	// ToolExecutionSequential runs every tool call in source order.
	ToolExecutionSequential ToolExecutionMode = "sequential"
)

// ToolExecutionModeProvider lets a tool opt into sequential execution within a batch.
type ToolExecutionModeProvider interface {
	ExecutionMode() ToolExecutionMode
}

// ToolBatchRuntime is implemented by tools/runtime for pi-style batches:
// PreflightTool runs policy and BeforeTool serially; RunToolBody executes the tool body.
type ToolBatchRuntime interface {
	ToolRuntime
	PreflightTool(ctx context.Context, call ToolCall) (result ToolResult, updatedCall ToolCall, runBody bool, err error)
	RunToolBody(ctx context.Context, call ToolCall) (ToolResult, error)
}
