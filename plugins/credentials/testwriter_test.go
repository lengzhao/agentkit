package credentials

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/cap/credentials"
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

// setTestManifests points an integrations store at test manifest files. Production
// always scans the fixed workspace defaults (global:mcp.json / global:api.json), so
// tests override the paths directly instead of via a config knob.
func setTestManifests(t *testing.T, store credentials.Store, paths ...string) {
	t.Helper()
	s, ok := store.(*integrationStore)
	if !ok {
		t.Fatalf("store is %T, want *integrationStore", store)
	}
	s.manifestPaths = append([]string(nil), paths...)
	if err := s.reloadManifest(context.Background()); err != nil {
		t.Fatal(err)
	}
}
