//go:build integration

package integration_test

import (
	"testing"

	capconfigfile "github.com/lengzhao/agentkit/cap/configfile"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtconfigfile "github.com/lengzhao/agentkit/runtime/configfile"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// newTestConfigFileWriter 返回 /add 命令测试用的标准原子写实现（test-only 依赖 runtime 实现）。
func newTestConfigFileWriter() capconfigfile.Writer {
	w, err := rtconfigfile.New(struct{}{}, struct{}{})
	if err != nil {
		panic(err)
	}
	return w
}

// integrationFS builds an unrestricted filesystem/local over a static root.
func integrationFS(t *testing.T, root string) filesystem.Service {
	t.Helper()
	return integrationFSOver(t, rtworkspace.Static(root))
}

// integrationFSOver builds an unrestricted filesystem/local delegating to ws.
func integrationFSOver(t *testing.T, ws workspace.Service) filesystem.Service {
	t.Helper()
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: ".", Unrestricted: true}, rtfilesystem.Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}
