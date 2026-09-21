package smoke_test

import (
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// smokeFS builds an unrestricted filesystem/local over a static workspace root.
func smokeFS(t *testing.T, root string) filesystem.Service {
	t.Helper()
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: ".", Unrestricted: true}, rtfilesystem.Deps{Workspace: rtworkspace.Static(root)})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}
