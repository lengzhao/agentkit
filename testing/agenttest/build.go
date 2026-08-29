package agenttest

import (
	"context"
	"testing"

	"github.com/lengzhao/pluginkit/build"
)

// Build constructs a plugin instance from a pluginkit graph fragment.
func Build[T any](t *testing.T, graph map[string]any, rootID string) T {
	t.Helper()
	inst, _, err := build.Build[T](context.Background(), graph, rootID)
	if err != nil {
		t.Fatalf("build %s: %v", rootID, err)
	}
	return inst
}
