package testutil

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// CallTool executes a tool with JSON input and returns raw JSON output.
// Deprecated: prefer agenttest.CallTool directly in new tests.
func CallTool(t *testing.T, ctx context.Context, tl agentkit.Tool, input string) string {
	return agenttest.CallTool(t, ctx, tl, input)
}
