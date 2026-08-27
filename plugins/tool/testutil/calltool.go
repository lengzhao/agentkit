package testutil

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
)

func CallTool(t *testing.T, ctx context.Context, tl agentkit.Tool, input string) string {
	t.Helper()
	result, err := tl.Call(ctx, json.RawMessage(input))
	if err != nil {
		t.Fatalf("call %s: %v", tl.Name(), err)
	}
	return result
}
