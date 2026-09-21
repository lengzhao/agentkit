package workspace_test

import (
	"context"
	"testing"

	rtws "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestResolveFileEmptyPath(t *testing.T) {
	t.Parallel()
	_, err := rtws.ResolveFile(context.Background(), rtws.Static(t.TempDir()), "")
	if err != rtws.ErrEmptyPath {
		t.Fatalf("err = %v", err)
	}
}
