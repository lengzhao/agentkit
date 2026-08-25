package config_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/config"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
)

// chainOnly lists presets that extend another preset's instances and therefore
// cannot build on the L0 base alone. TestChainedPresetsBuild covers them; the
// value is the reason, so a future reader knows this is by design rather than a
// preset someone forgot to finish.
var chainOnly = map[string]string{
	"cron.yaml": "extends the autonomous tool set with tool/schedule",
}

// TestPresetsBuild assembles every shipped preset against the L0 base config.
// A preset that references a renamed kind or a missing dep only fails at
// startup otherwise, which is the worst place to find out.
func TestPresetsBuild(t *testing.T) {
	// Presets wire llm/openai-compatible with apiKeyRef env:OPENAI_API_KEY, and
	// credential resolution happens at construction time.
	t.Setenv("OPENAI_API_KEY", "test-key")

	presets, err := filepath.Glob(filepath.Join("..", "presets", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) == 0 {
		t.Fatal("no presets found")
	}

	for _, overlay := range presets {
		t.Run(filepath.Base(overlay), func(t *testing.T) {
			if reason, ok := chainOnly[filepath.Base(overlay)]; ok {
				t.Skipf("chain-only preset: %s", reason)
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

// TestChainedPresetsBuild covers the documented overlay chains. The transport
// presets are thin on purpose, so the combination is what users actually run.
func TestChainedPresetsBuild(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	chains := [][]string{
		{"autonomous.yaml", "worker.yaml"},
		{"autonomous.yaml", "daemon.yaml"},
		{"autonomous.yaml", "cron.yaml"},
	}
	for _, chain := range chains {
		t.Run(strings.Join(chain, "+"), func(t *testing.T) {
			overlays := make([]string, 0, len(chain))
			for _, name := range chain {
				overlays = append(overlays, filepath.Join("..", "presets", name))
			}
			doc, err := config.LoadDocument(filepath.Join("..", "config.base.yaml"), overlays...)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if _, _, err := build.Build[agentkit.Runner](context.Background(), doc.ToGraph(), doc.RootID); err != nil {
				t.Fatalf("build: %v", err)
			}
		})
	}
}

// TestOverlayChainOrderWins pins the merge rule the chained presets rely on.
func TestOverlayChainOrderWins(t *testing.T) {
	base := []byte("runner.default:\n  use: runner\n  config:\n    shutdownTimeoutSeconds: 1\n")
	first := []byte("runner.default:\n  use: runner\n  config:\n    shutdownTimeoutSeconds: 2\n")
	second := []byte("runner.default:\n  use: runner\n  config:\n    shutdownTimeoutSeconds: 3\n")

	merged, err := config.ResolveYAML(base, first, second)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(string(merged), "shutdownTimeoutSeconds: 3") {
		t.Fatalf("last overlay should win:\n%s", merged)
	}
}

// TestBaseConfigBuilds covers the default no-overlay path.
func TestBaseConfigBuilds(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")

	doc, err := config.LoadDocument(filepath.Join("..", "config.base.yaml"), "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, _, err := build.Build[agentkit.Runner](context.Background(), doc.ToGraph(), doc.RootID); err != nil {
		t.Fatalf("build: %v", err)
	}
}
