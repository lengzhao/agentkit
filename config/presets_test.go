package config_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/config"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
)

// presetsChainOnly lists overlays that extend another preset and cannot build on L0 alone.
var presetsChainOnly = map[string]string{
	"worker.yaml":         "requires autonomous capability stack",
	"daemon.yaml":         "requires autonomous capability stack",
	"cron.yaml":           "requires autonomous capability stack",
	"subagent-smoke.yaml": "requires subagent capability stack",
	"web-smoke.yaml":      "requires web capability stack",
	"p1-context.yaml":     "capability fragment, not a full overlay",
}

func TestPresetsBuild(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	presets, err := filepath.Glob(filepath.Join("..", "presets", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) == 0 {
		t.Fatal("no preset configs found")
	}

	for _, overlay := range presets {
		name := filepath.Base(overlay)
		t.Run(name, func(t *testing.T) {
			if reason, ok := presetsChainOnly[name]; ok {
				t.Skipf("chain-only or fragment preset: %s", reason)
			}
			doc, err := config.LoadDocument(filepath.Join("..", "config.base.yaml"), overlay)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if _, _, err := build.Build[agentkit.Runner](context.Background(), doc.ToGraph(), doc.RootID); err != nil {
				t.Fatalf("build: %v", err)
			}
		})
	}
}

func TestPresetsChainedBuild(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	chains := [][]string{
		{"autonomous.yaml", "worker.yaml"},
		{"autonomous.yaml", "daemon.yaml"},
		{"autonomous.yaml", "cron.yaml"},
		{"subagent.yaml", "subagent-smoke.yaml"},
		{"web.yaml", "web-smoke.yaml"},
	}
	for _, chain := range chains {
		t.Run(strings.Join(chain, "+"), func(t *testing.T) {
			overlays := make([]string, 0, len(chain))
			for _, name := range chain {
				overlays = append(overlays, filepath.Join("presets", name))
			}
			doc, err := config.LoadDocument("config.base.yaml", overlays...)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if _, _, err := build.Build[agentkit.Runner](context.Background(), doc.ToGraph(), doc.RootID); err != nil {
				t.Fatalf("build: %v", err)
			}
		})
	}
}
