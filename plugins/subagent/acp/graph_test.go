package acp

import (
	"fmt"
	"testing"

	"github.com/lengzhao/pluginkit/build"
	"github.com/lengzhao/pluginkit/manager"
)

func TestPluginGraphWithoutMCP(t *testing.T) {
	raw := fmt.Sprintf(`child:
  use: subagent/acp
  config:
    agents:
      - name: reviewer
        description: review code
        acp:
          command: [not-started-at-build]
  deps:
    workspace: ws
    fs: fs
    sessionStore: store
    telemetry: tk
ws:
  use: workspace/default
  config:
    global: %q
    local: %q
fs:
  use: filesystem/local
  deps:
    workspace: ws
store:
  use: session/store
  config:
    dir: sessions
  deps:
    workspace: ws
tk:
  use: telemetry/toolkit
`, t.TempDir(), t.TempDir())
	doc, err := manager.FromYAML([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := build.Build[*Spawner](t.Context(), doc.ToGraph(), "child")
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	defs, err := s.Definitions(t.Context())
	if err != nil || len(defs) != 1 {
		t.Fatalf("catalog: %+v %v", defs, err)
	}
}
