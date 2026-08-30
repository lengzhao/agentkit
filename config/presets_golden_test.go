package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/config"
)

var presetsGoldenChainOnly = map[string]bool{
	"worker.yaml":     true,
	"daemon.yaml":     true,
	"cron.yaml":       true,
	"web-smoke.yaml":  true,
	"p1-context.yaml": true,
}

func TestPresetsResolveGolden(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk-test")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk-test")

	presets, err := filepath.Glob(filepath.Join("..", "presets", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, overlay := range presets {
		name := filepath.Base(overlay)
		if presetsGoldenChainOnly[name] {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := config.ResolveFiles(filepath.Join("..", "config.base.yaml"), overlay)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			goldenPath := filepath.Join("testdata", "presets", strings.TrimSuffix(name, ".yaml")+".resolved.yaml")
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %q: %v", goldenPath, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("resolved output differs from golden %q (run: go run /tmp/gen_golden.go from repo root to refresh)", goldenPath)
			}
		})
	}
}
