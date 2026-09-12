//go:build ignore

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit/config"
)

func main() {
	skip := map[string]bool{
		"worker.yaml": true, "daemon.yaml": true, "cron.yaml": true,
		"web-smoke.yaml": true, "p1-context.yaml": true,
	}
	matches, err := filepath.Glob("../presets/*.yaml")
	if err != nil {
		panic(err)
	}
	for _, overlay := range matches {
		name := filepath.Base(overlay)
		if skip[name] {
			continue
		}
		got, err := config.ResolveFiles("../config.base.yaml", overlay)
		if err != nil {
			panic(err)
		}
		out := filepath.Join("testdata/presets", strings.TrimSuffix(name, ".yaml")+".resolved.yaml")
		if err := os.WriteFile(out, got, 0o644); err != nil {
			panic(err)
		}
	}
}
