package config_test

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/config"
)

// Chain-only presets skip full golden files; assert critical deps after memory/learning split.
func TestChainOnlyPresetsMemoryDeps(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk-test")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk-test")

	for _, preset := range []string{"cron.yaml", "daemon.yaml", "worker.yaml", "web-smoke.yaml"} {
		t.Run(preset, func(t *testing.T) {
			t.Parallel()
			got, err := config.ResolveFiles("../config.base.yaml", "../presets/"+preset)
			if err != nil {
				t.Fatal(err)
			}
			s := string(got)
			idx := strings.Index(s, "prompt.memory.default:")
			if idx < 0 {
				t.Fatal("missing prompt.memory.default")
			}
			block := s[idx : idx+280]
			if !strings.Contains(block, "memory: memory.default") {
				t.Fatalf("prompt.memory must dep memory.default, got:\n%s", block)
			}
			if strings.Contains(block, "learning: learning.default") {
				t.Fatalf("prompt.memory must not dep learning.default:\n%s", block)
			}
		})
	}
}
