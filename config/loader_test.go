package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/config"
	"github.com/lengzhao/pluginkit/manager"
)

func TestMergeYAMLOverlayReplacesInstance(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    workspace: workspace.default
workspace.default:
  use: workspace/default
  config:
    root: ~/.agentkit
llm.default:
  use: llm/openai-compatible
  config:
    model: gpt-5.4
`)
	overlay := []byte(`workspace.default:
  use: workspace/default
  config:
    root: .
`)
	resolved, err := config.ResolveYAML(base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	workspace, ok := doc.Shared["workspace.default"]
	if !ok {
		t.Fatal("missing workspace.default")
	}
	if workspace.Config["root"] != "." {
		t.Fatalf("workspace.root=%v", workspace.Config["root"])
	}
	if _, ok := doc.Shared["llm.default"]; ok {
		t.Fatal("expected unreachable llm.default to be pruned")
	}
}

func TestLoadDocumentBaseOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	basePath := filepath.Join(dir, "config.base.yaml")
	if err := os.WriteFile(basePath, []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
platform.default:
  use: platform/cli
`), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := config.LoadDocument(basePath, filepath.Join(dir, "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.RootID != "runner.default" {
		t.Fatalf("rootId=%q", doc.RootID)
	}
}
