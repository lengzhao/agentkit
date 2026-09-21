package credentials

import (
	"testing"

	capconfigfile "github.com/lengzhao/agentkit/cap/configfile"
	"github.com/lengzhao/agentkit/cap/filesystem"
	rtconfigfile "github.com/lengzhao/agentkit/runtime/configfile"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// testConfigFileWriter 是 /env add 测试用的标准原子写实现（test-only 依赖 runtime 实现）。
var testConfigFileWriter capconfigfile.Writer = func() capconfigfile.Writer {
	w, err := rtconfigfile.New(struct{}{}, struct{}{})
	if err != nil {
		panic(err)
	}
	return w
}()

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
