package agentkit

import (
	"context"
	"encoding/json"
)

// Tool is the model-visible consumer plugin type.
//
// Call receives only the raw arguments and returns model-visible text. The tool
// runtime stamps call identity onto a ToolResult before writing to session or
// sending back to the model.
type Tool interface {
	Name() string
	Description() string
	InputSchema() JSONSchema
	Call(context.Context, json.RawMessage) (string, error)
}

// ToolProvider supplies tools whose definitions may change between turns.
// Implementations re-read configuration or rediscover remote tools on each call.
type ToolProvider interface {
	ListTools(context.Context) ([]Tool, error)
}

// ToolPack is one plugin instance that exposes one or more model-visible tools.
type ToolPack []Tool

// Pack returns a ToolPack from individual tools.
func Pack(tools ...Tool) ToolPack {
	return tools
}

// First returns the only tool in a single-tool pack.
func First(pack ToolPack) Tool {
	if len(pack) == 0 {
		return nil
	}
	return pack[0]
}

type ToolSpec struct {
	Name        string
	Description string
	InputSchema JSONSchema
}

type ToolCall struct {
	ID    ToolCallID
	Name  string
	Input json.RawMessage
}

// ToolResult is a tool output bound to a specific call, used by session history,
// hooks, and LLM wire-up. Audit is populated by the tool runtime (policy deny,
// timeout, etc.), not by Tool.Call.
type ToolResult struct {
	ID      ToolCallID
	Name    string
	Content string
	Audit   map[string]string
}

// ResultFromCall attaches call identity to a tool output.
func ResultFromCall(call ToolCall, output string) ToolResult {
	return ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: output,
	}
}

type ToolRuntime interface {
	Visible(context.Context) ([]ToolSpec, error)
	Execute(context.Context, ToolCall) (ToolResult, error)
}
