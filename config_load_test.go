package agentkit_test

import (
	"context"
	"os"
	"testing"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
	"github.com/lengzhao/pluginkit/manager"
)

func TestPresetCodingYAMLLoadsAndBuilds(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("presets/coding-smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := manager.FromYAML(raw)
	if err != nil {
		t.Fatalf("from yaml: %v", err)
	}
	if doc.RootID != "runner" {
		t.Fatalf("rootId=%q", doc.RootID)
	}
	if doc.Plugin.Use != "runner" {
		t.Fatalf("root use=%q", doc.Plugin.Use)
	}

	graph := doc.ToGraph()
	if _, ok := graph["runner"]; !ok {
		t.Fatal("graph missing runner")
	}

	_, _, err = build.Build[agentkit.Runner](context.Background(), graph, doc.RootID)
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}

	roundTrip, err := doc.ToYAML()
	if err != nil {
		t.Fatalf("to yaml: %v", err)
	}
	reloaded, err := manager.FromYAML(roundTrip)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if reloaded.RootID != doc.RootID || reloaded.Plugin.Use != doc.Plugin.Use {
		t.Fatalf("round trip mismatch: %#v", reloaded)
	}
}
