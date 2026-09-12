//go:build ignore

// Regenerate testdata/presets/*.resolved.yaml after config.base.yaml changes:
//
//	cd config && OPENAI_API_KEY=test-key LANGFUSE_PUBLIC_KEY=pk-test LANGFUSE_SECRET_KEY=sk-test go run regen_presets_golden.go
package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/config"
)

var chainOnly = map[string]bool{
	"worker.yaml":     true,
	"daemon.yaml":     true,
	"cron.yaml":       true,
	"web-smoke.yaml":  true,
	"p1-context.yaml": true,
}

func main() {
	base := filepath.Join("..", "config.base.yaml")
	presets, err := filepath.Glob(filepath.Join("..", "presets", "*.yaml"))
	if err != nil {
		panic(err)
	}
	outDir := filepath.Join("testdata", "presets")
	for _, overlay := range presets {
		name := filepath.Base(overlay)
		if chainOnly[name] {
			continue
		}
		got, err := config.ResolveFiles(base, overlay)
		if err != nil {
			panic(name + ": " + err.Error())
		}
		out := filepath.Join(outDir, strings.TrimSuffix(name, ".yaml")+".resolved.yaml")
		if err := os.WriteFile(out, got, 0o644); err != nil {
			panic(err)
		}
	}
}
