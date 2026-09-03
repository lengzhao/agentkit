package config_test

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/config"
	_ "github.com/lengzhao/agentkit/plugins/credentials"
)

func TestEnvInterpolationUsesCredentialConfigEnv(t *testing.T) {
	t.Setenv("AGENTHUB_URL", "")
	t.Setenv("AGENTHUB_API_KEY", "")

	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
    loop: loop.default
    init:
      - bootstrap.skills-sync.default
loop.default:
  use: loop/default
platform.default:
  use: platform/cli
credentials.default:
  use: credentials/env
  config:
    env:
      AGENTHUB_URL: https://agenthub.example.com
      AGENTHUB_API_KEY: pag_test
bootstrap.skills-sync.default:
  use: bootstrap/skills-sync
  deps:
    command: command.skills-sync.default
command.skills-sync.default:
  use: command/skills-sync
  config:
    urlRef: ${var:AGENTHUB_URL}
    apiKeyRef: ${env:AGENTHUB_API_KEY}
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	body := string(resolved)
	if !strings.Contains(body, "command.skills-sync.default") {
		t.Fatalf("command.skills-sync.default should remain when credentials.default.config.env provides AGENTHUB_*:\n%s", body)
	}
	if strings.Contains(body, "${var:AGENTHUB_URL}") {
		t.Fatalf("urlRef should be interpolated:\n%s", body)
	}
	if !strings.Contains(body, "https://agenthub.example.com") {
		t.Fatalf("resolved config should contain AGENTHUB_URL value:\n%s", body)
	}
	if !strings.Contains(body, "env:AGENTHUB_API_KEY") {
		t.Fatalf("apiKeyRef should expand to env:AGENTHUB_API_KEY:\n%s", body)
	}
	if strings.Contains(body, "pag_test") {
		t.Fatalf("apiKeyRef should not inline secret value:\n%s", body)
	}
}

func TestEnvInterpolationMissingPrunesWhenCredentialConfigEnvAbsent(t *testing.T) {
	t.Setenv("AGENTHUB_URL", "")
	t.Setenv("AGENTHUB_API_KEY", "")

	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
    loop: loop.default
    init:
      - bootstrap.skills-sync.default
loop.default:
  use: loop/default
platform.default:
  use: platform/cli
bootstrap.skills-sync.default:
  use: bootstrap/skills-sync
  deps:
    command: command.skills-sync.default
command.skills-sync.default:
  use: command/skills-sync
  config:
    urlRef: ${var:AGENTHUB_URL}
    apiKeyRef: ${env:AGENTHUB_API_KEY}
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(resolved), "command.skills-sync.default") {
		t.Fatalf("command.skills-sync.default should be pruned when AGENTHUB_* is missing:\n%s", resolved)
	}
}
