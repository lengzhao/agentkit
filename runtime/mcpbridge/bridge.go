// Package mcpbridge adapts ToolRuntime to MCP without owning transport,
// authentication, execution identity, or an ACP session.
package mcpbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/lengzhao/agentkit"
	mcp "github.com/mark3labs/mcp-go/mcp"
)

// Bridge is stateless; the runtime owns policy, approval, timeout and visibility.
// Concurrent use is safe when the injected runtime supports it. Hosts must pass
// trusted execution contexts, not identities decoded from untrusted headers.
type Bridge struct{ runtime agentkit.ToolRuntime }

func New(runtime agentkit.ToolRuntime) (*Bridge, error) {
	if runtime == nil {
		return nil, fmt.Errorf("MCP bridge requires a tool runtime")
	}
	return &Bridge{runtime: runtime}, nil
}

// Tools renders a fresh, deterministic catalog for this execution context.
// Hosts own registry refresh and synchronization with concurrent tool calls.
func (b *Bridge) Tools(ctx context.Context) ([]mcp.Tool, error) {
	specs, err := b.runtime.Visible(ctx)
	if err != nil {
		return nil, fmt.Errorf("MCP visible: %w", err)
	}
	out := make([]mcp.Tool, 0, len(specs))
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) == "" || seen[spec.Name] {
			return nil, fmt.Errorf("MCP invalid or duplicate tool name %q", spec.Name)
		}
		seen[spec.Name] = true
		var raw []byte
		if len(spec.InputSchema.Raw) > 0 {
			raw, err = json.Marshal(spec.InputSchema.Raw)
		} else {
			raw, err = json.Marshal(spec.InputSchema)
		}
		if err != nil {
			return nil, fmt.Errorf("MCP schema %s: %w", spec.Name, err)
		}
		out = append(out, mcp.NewToolWithRawSchema(spec.Name, spec.Description, raw))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CallTool always executes through the runtime, never through a raw Tool.Call.
// A runtime error is returned both as an MCP isError result and as its original
// Go cause so hosts can honor cancellation/turn-abort semantics. MCP handlers
// should send the result as a tool error (not a JSON-RPC protocol error).
func (b *Bridge) CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fail := func(err error) (*mcp.CallToolResult, error) { return mcp.NewToolResultError(err.Error()), err }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	args := req.Params.Arguments
	if args == nil {
		args = map[string]any{}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return fail(err)
	}
	call := agentkit.ToolCall{ID: agentkit.ToolCallID(uuid.NewString()), Name: req.Params.Name, Input: raw}
	result, err := b.runtime.Execute(ctx, call)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		text := err.Error()
		if result.Content != "" {
			text = result.Content + "\n" + text
		}
		return mcp.NewToolResultError(text), err
	}
	switch result.Audit["decision"] {
	case "deny", "timeout", "error":
		return mcp.NewToolResultError(result.Content), nil
	}
	return mcp.NewToolResultText(result.Content), nil
}
