package memory

import (
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
)

// testFS builds a filesystem/local rooted at the workspace local root.
func testFS(t *testing.T, ws workspace.Service) filesystem.Service {
	t.Helper()
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: "."}, rtfilesystem.Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}
