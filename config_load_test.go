package agentkit_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/config"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
)

func TestConfigBaseLoadsAndBuilds(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	doc, err := config.LoadDocument(config.DefaultBasePath, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.RootID != "runner.default" {
		t.Fatalf("rootId=%q", doc.RootID)
	}
	if doc.Plugin.Use != "runner" {
		t.Fatalf("root use=%q", doc.Plugin.Use)
	}

	graph := doc.ToGraph()
	if _, ok := graph["runner.default"]; !ok {
		t.Fatal("graph missing runner.default")
	}

	_, _, err = build.Build[agentkit.Runner](context.Background(), graph, doc.RootID)
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
}

func TestPresetCodingOverlayBuilds(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	doc, err := config.LoadDocument(config.DefaultBasePath, "presets/coding.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if doc.RootID != "runner.default" {
		t.Fatalf("rootId=%q", doc.RootID)
	}

	workspace, ok := doc.Shared["workspace.default"]
	if !ok {
		t.Fatal("missing workspace.default")
	}
	if workspace.Config["root"] != "." {
		t.Fatalf("workspace.root=%v", workspace.Config["root"])
	}

	_, _, err = build.Build[agentkit.Runner](context.Background(), doc.ToGraph(), doc.RootID)
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
}

func TestPresetCodingSmokeOverlayBuilds(t *testing.T) {
	t.Parallel()
	doc, err := config.LoadDocument(config.DefaultBasePath, "presets/coding-smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if doc.RootID != "runner.default" {
		t.Fatalf("rootId=%q", doc.RootID)
	}

	_, _, err = build.Build[agentkit.Runner](context.Background(), doc.ToGraph(), doc.RootID)
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
}
