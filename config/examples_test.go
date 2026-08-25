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

// examplesChainOnly lists example configs that extend another example and
// cannot build on the L0 base alone.
var examplesChainOnly = map[string]string{
	"headless-worker.yaml": "requires autonomous capability stack",
	"interval-daemon.yaml":   "requires autonomous capability stack",
	"cron-daemon.yaml":       "requires autonomous capability stack",
}

func TestExamplesBuild(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	examples, err := filepath.Glob(filepath.Join("..", "examples", "config", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) == 0 {
		t.Fatal("no example configs found")
	}

	for _, overlay := range examples {
		name := filepath.Base(overlay)
		t.Run(name, func(t *testing.T) {
			if reason, ok := examplesChainOnly[name]; ok {
				t.Skipf("chain-only example: %s", reason)
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

func TestExamplesChainedBuild(t *testing.T) {
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
		{"autonomous.yaml", "headless-worker.yaml"},
		{"autonomous.yaml", "interval-daemon.yaml"},
		{"autonomous.yaml", "cron-daemon.yaml"},
	}
	for _, chain := range chains {
		t.Run(strings.Join(chain, "+"), func(t *testing.T) {
			overlays := make([]string, 0, len(chain))
			for _, name := range chain {
				overlays = append(overlays, filepath.Join("examples", "config", name))
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
