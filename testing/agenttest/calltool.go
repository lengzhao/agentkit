package agenttest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
)

// CallTool executes a tool with JSON input and returns raw JSON output.
func CallTool(t *testing.T, ctx context.Context, tool agentkit.Tool, input string) string {
	t.Helper()
	result, err := tool.Call(ctx, json.RawMessage(input))
	if err != nil {
		t.Fatalf("call %s: %v", tool.Name(), err)
	}
	return result
}
