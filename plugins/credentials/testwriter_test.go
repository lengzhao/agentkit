package credentials

import (
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// testEnvFS builds an unrestricted filesystem/local over a temp dir (tests pass
// absolute paths in Config.Files).
func testEnvFS(t *testing.T) filesystem.Service {
	t.Helper()
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: ".", Unrestricted: true}, rtfilesystem.Deps{Workspace: rtworkspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}
