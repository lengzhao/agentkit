package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/config"
)

func TestRedactInstancesKeepsCredentialRefs(t *testing.T) {
	raw := map[string]any{
		"llm.default": map[string]any{
			"config": map[string]any{
				"apiKeyRef": "env:OPENAI_API_KEY",
			},
		},
		"platform.default": map[string]any{
			"config": map[string]any{
				"token": "actual-secret",
			},
		},
	}
	config.RedactInstances(raw)

	llm := raw["llm.default"].(map[string]any)["config"].(map[string]any)
	if llm["apiKeyRef"] != "env:OPENAI_API_KEY" {
		t.Fatalf("apiKeyRef=%v, want credential ref preserved", llm["apiKeyRef"])
	}
	platform := raw["platform.default"].(map[string]any)["config"].(map[string]any)
	if platform["token"] != "[REDACTED]" {
		t.Fatalf("token=%v, want redacted", platform["token"])
	}
}

func TestDumpResolvedYAMLRedactsByDefault(t *testing.T) {
	t.Setenv("TEST_AGENTKIT_DUMP_TOKEN", "super-secret")

	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
platform.default:
  use: platform/cli
  config:
    token: ${env:TEST_AGENTKIT_DUMP_TOKEN}
    apiKeyRef: env:OPENAI_API_KEY
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	_ = resolved

	dir := t.TempDir()
	basePath := dir + "/base.yaml"
	if err := os.WriteFile(basePath, base, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := config.DumpResolvedYAML(basePath, nil, config.DumpOptions{Redact: true})
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if strings.Contains(out, "super-secret") {
		t.Fatalf("dump leaked secret: %s", out)
	}
	if !strings.Contains(out, "env:OPENAI_API_KEY") {
		t.Fatalf("dump should keep credential ref: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("dump should redact token: %s", out)
	}
}
