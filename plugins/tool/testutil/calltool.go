package testutil

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func CallTool(t *testing.T, ctx context.Context, tl agentkit.Tool, input string) string {
	t.Helper()
	result, err := tl.Call(ctx, agentkit.ToolCall{ID: "call", Name: tl.Name(), Input: json.RawMessage(input)})
	if err != nil {
		t.Fatalf("call %s: %v", tl.Name(), err)
	}
	var b strings.Builder
	for _, part := range result.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}
