package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/config"
	"github.com/lengzhao/pluginkit/manager"
)

func TestMergeYAMLDeepMergesConfigWhenUseUnchanged(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    llm: llm.default
llm.default:
  use: llm/openai-compatible
  config:
    api: chat
    model: gpt-4
    retry:
      provider: default
`)
	overlay := []byte(`llm.default:
  config:
    model: gpt-5.4
    baseUrl: https://example.com/v1
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	cfg := doc.Shared["llm.default"].Config
	if cfg["api"] != "chat" {
		t.Fatalf("api=%v", cfg["api"])
	}
	if cfg["model"] != "gpt-5.4" {
		t.Fatalf("model=%v", cfg["model"])
	}
	retry, ok := cfg["retry"].(map[string]any)
	if !ok || retry["provider"] != "default" {
		t.Fatalf("retry=%v", cfg["retry"])
	}
}

func TestMergeYAMLReplacesNodeWhenUseChanges(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
platform.default:
  use: platform/cli
  config:
    once: false
  deps:
    commands: commands.default
`)
	overlay := []byte(`platform.default:
  use: platform/chat-api
  config:
    listenAddr: ":8030"
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	node := doc.Shared["platform.default"]
	if node.Use != "platform/chat-api" {
		t.Fatalf("use=%q", node.Use)
	}
	if node.Deps != nil {
		t.Fatalf("deps=%v", node.Deps)
	}
}

func TestMergeYAMLDepsAppendSuffix(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    loop: loop.default
loop.default:
  use: loop/default
  deps:
    agents:
      - agent.assistant.default
agent.assistant.default:
  use: agent/coding
agent.meetingbot.default:
  use: agent/coding
`)
	overlay := []byte(`loop.default:
  deps:
    agents+:
      - agent.meetingbot.default
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	agents := doc.Shared["loop.default"].Deps["agents"].([]any)
	if len(agents) != 2 {
		t.Fatalf("agents=%v", agents)
	}
}

func TestMergeYAMLDepsRemoveSuffix(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    loop: loop.default
loop.default:
  use: loop/default
  deps:
    agents:
      - agent.assistant.default
      - agent.meetingbot.default
      - agent.worker.default
agent.assistant.default:
  use: agent/coding
agent.meetingbot.default:
  use: agent/coding
agent.worker.default:
  use: agent/coding
`)
	overlay := []byte(`loop.default:
  deps:
    agents-:
      - agent.meetingbot.default
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	agents := doc.Shared["loop.default"].Deps["agents"].([]any)
	if len(agents) != 2 {
		t.Fatalf("agents=%v", agents)
	}
	if agents[0] != "agent.assistant.default" || agents[1] != "agent.worker.default" {
		t.Fatalf("agents=%v", agents)
	}
}

func TestMergeYAMLDepsRemoveSuffixMultiple(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    loop: loop.default
loop.default:
  use: loop/default
  deps:
    agents:
      - agent.a.default
      - agent.b.default
      - agent.c.default
agent.a.default:
  use: agent/coding
agent.b.default:
  use: agent/coding
agent.c.default:
  use: agent/coding
`)
	overlay := []byte(`loop.default:
  deps:
    agents-:
      - agent.a.default
      - agent.c.default
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	agents := doc.Shared["loop.default"].Deps["agents"].([]any)
	if len(agents) != 1 || agents[0] != "agent.b.default" {
		t.Fatalf("agents=%v", agents)
	}
}

func TestMergeYAMLDepsRemoveSuffixNoMatch(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    loop: loop.default
loop.default:
  use: loop/default
  deps:
    agents:
      - agent.assistant.default
agent.assistant.default:
  use: agent/coding
`)
	overlay := []byte(`loop.default:
  deps:
    agents-:
      - agent.missing.default
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	agents := doc.Shared["loop.default"].Deps["agents"].([]any)
	if len(agents) != 1 || agents[0] != "agent.assistant.default" {
		t.Fatalf("agents=%v", agents)
	}
}

func TestMergeYAMLDepsAppendThenRemoveSuffix(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    loop: loop.default
loop.default:
  use: loop/default
  deps:
    agents:
      - agent.assistant.default
agent.assistant.default:
  use: agent/coding
agent.meetingbot.default:
  use: agent/coding
agent.worker.default:
  use: agent/coding
`)
	overlay := []byte(`loop.default:
  deps:
    agents+:
      - agent.meetingbot.default
      - agent.worker.default
    agents-:
      - agent.worker.default
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	agents := doc.Shared["loop.default"].Deps["agents"].([]any)
	if len(agents) != 2 {
		t.Fatalf("agents=%v", agents)
	}
	if agents[0] != "agent.assistant.default" || agents[1] != "agent.meetingbot.default" {
		t.Fatalf("agents=%v", agents)
	}
}

func TestMergeYAMLNullDeletesKey(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    telemetry: telemetry.default
    loop: loop.default
telemetry.default:
  use: telemetry/none
`)
	overlay := []byte(`runner.default:
  deps:
    telemetry: null
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Shared["runner.default"].Deps["telemetry"]; ok {
		t.Fatal("telemetry dep should be deleted")
	}
}

func TestExtendsMergesParentNode(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    loop: loop.default
loop.default:
  use: loop/default
  deps:
    agents:
      - agent.assistant.default
      - agent.meetingbot.default
agent.assistant.default:
  use: agent/coding
  config:
    id: assistant
    model: gpt-5.4
  deps:
    prompt: prompt.default
prompt.default:
  use: prompt/assembler/default
agent.meetingbot.default:
  extends: agent.assistant.default
  config:
    id: meetingbot
  deps:
    prompt: prompt.meetingbot.default
prompt.meetingbot.default:
  use: prompt/assembler/default
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	node := doc.Shared["agent.meetingbot.default"]
	if node.Config["id"] != "meetingbot" {
		t.Fatalf("id=%v", node.Config["id"])
	}
	if node.Config["model"] != "gpt-5.4" {
		t.Fatalf("model=%v", node.Config["model"])
	}
	if node.Deps["prompt"] != "prompt.meetingbot.default" {
		t.Fatalf("prompt dep=%v", node.Deps["prompt"])
	}
}

func TestExtendsCycleFails(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
a.default:
  extends: b.default
  use: agent/coding
b.default:
  extends: a.default
  use: agent/coding
`)
	_, err := config.ResolveYAML(".", base)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("err=%v", err)
	}
}

func TestVarInterpolation(t *testing.T) {
	t.Setenv("TEST_AGENTKIT_URL", "https://example.com")
	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
platform.default:
  use: platform/cli
  config:
    baseUrl: ${var:TEST_AGENTKIT_URL}
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Shared["platform.default"].Config["baseUrl"] != "https://example.com" {
		t.Fatalf("baseUrl=%v", doc.Shared["platform.default"].Config["baseUrl"])
	}
}

func TestEnvInterpolation(t *testing.T) {
	t.Setenv("TEST_AGENTKIT_TOKEN", "secret-value")
	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
platform.default:
  use: platform/cli
  config:
    token: ${env:TEST_AGENTKIT_TOKEN}
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Shared["platform.default"].Config["token"] != "env:TEST_AGENTKIT_TOKEN" {
		t.Fatalf("token=%v", doc.Shared["platform.default"].Config["token"])
	}
}

func TestEnvInterpolationDefaultValue(t *testing.T) {
	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
platform.default:
  use: platform/cli
  config:
    token: ${env:MISSING_AGENTKIT_TOKEN:-fallback}
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Shared["platform.default"].Config["token"] != "fallback" {
		t.Fatalf("token=%v", doc.Shared["platform.default"].Config["token"])
	}
}

func TestFileInterpolationRelativeToInterpDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	contentPath := filepath.Join(dir, "persona.md")
	if err := os.WriteFile(contentPath, []byte("hello persona"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := []byte(`runner.default:
  use: runner
  deps:
    prompt: prompt.default
prompt.default:
  use: prompt/section/static
  config:
    name: persona
    content: ${file:persona.md}
`)
	resolved, err := config.ResolveYAML(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Shared["prompt.default"].Config["content"] != "hello persona" {
		t.Fatalf("content=%q", doc.Shared["prompt.default"].Config["content"])
	}
}

func TestMergeYAMLDeepMergePreservesDepsUnlessCleared(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    credentials: credentials.default
credentials.default:
  use: credentials/env
  config:
    files:
      - local:.env
  deps:
    workspace: workspace.default
workspace.default:
  use: workspace/default
`)
	overlay := []byte(`credentials.default:
  config:
    files:
      - .env
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	node := doc.Shared["credentials.default"]
	files, ok := node.Config["files"].([]any)
	if !ok || len(files) != 1 || files[0] != ".env" {
		t.Fatalf("files=%v", node.Config["files"])
	}
	if node.Deps["workspace"] != "workspace.default" {
		t.Fatalf("deps=%v", node.Deps)
	}
}

func TestMergeYAMLNullClearsDeps(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    credentials: credentials.default
credentials.default:
  use: credentials/env
  config:
    files:
      - local:.env
  deps:
    workspace: workspace.default
`)
	overlay := []byte(`credentials.default:
  config:
    files:
      - .env
  deps:
    workspace: null
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	node := doc.Shared["credentials.default"]
	if _, ok := node.Deps["workspace"]; ok {
		t.Fatalf("workspace dep should be cleared: deps=%v", node.Deps)
	}
}

func TestLoadDocumentBaseOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	basePath := filepath.Join(dir, "config.base.yaml")
	if err := os.WriteFile(basePath, []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
platform.default:
  use: platform/cli
`), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := config.LoadDocument(basePath, filepath.Join(dir, "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.RootID != "runner.default" {
		t.Fatalf("rootId=%q", doc.RootID)
	}
}

func TestEnvInterpolationMissingPrunesOptionalPlatform(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
    loop: loop.default
loop.default:
  use: loop/default
  deps:
    agents:
      - agent.assistant.default
agent.assistant.default:
  use: agent/coding
platform.default:
  use: platform/cli
platform.slack:
  use: platform/slack
  config:
    botToken: ${env:SLACK_BOT_TOKEN}
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Shared["platform.slack"]; ok {
		t.Fatal("platform.slack should be pruned when env is missing")
	}
	if doc.Shared["platform.default"].Use != "platform/cli" {
		t.Fatalf("platform.default should remain")
	}
}

func TestOverlayNullDisablesInstanceAndPrunesDeps(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.multiplex
platform.multiplex:
  use: platform/multiplex
  deps:
    platforms:
      - platform.chat-api
      - platform.http
platform.chat-api:
  use: platform/chat-api
platform.http:
  use: platform/http
`)
	overlay := []byte(`platform.chat-api: null
`)
	resolved, err := config.ResolveYAML(".", base, overlay)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Shared["platform.chat-api"]; ok {
		t.Fatal("platform.chat-api should be pruned after explicit disable")
	}
	platforms := doc.Shared["platform.multiplex"].Deps["platforms"].([]any)
	if len(platforms) != 1 || platforms[0] != "platform.http" {
		t.Fatalf("platforms=%v", platforms)
	}
}

func TestBaseNullInstanceIsInvalid(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.chat-api
platform.chat-api: null
`)
	_, err := config.ResolveYAML(".", base)
	if err == nil {
		t.Fatal("expected error for null instance in base config")
	}
	if !strings.Contains(err.Error(), "null can only be used in overlays") {
		t.Fatalf("err=%v", err)
	}
}

func TestOverlayNullUnknownInstanceIsInvalid(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
`)
	overlay := []byte(`platform.typo: null
`)
	_, err := config.ResolveYAML(".", base, overlay)
	if err == nil {
		t.Fatal("expected error when overlay disables unknown instance")
	}
	if !strings.Contains(err.Error(), `overlay disables unknown instance "platform.typo"`) {
		t.Fatalf("err=%v", err)
	}
}

func TestEnvInterpolationMissingPrunesCascadeWhenDepsEmpty(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.multiplex
    loop: loop.default
loop.default:
  use: loop/default
platform.multiplex:
  use: platform/multiplex
  deps:
    platforms:
      - platform.slack
platform.slack:
  use: platform/slack
  config:
    botToken: ${env:SLACK_BOT_TOKEN}
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Shared["platform.multiplex"]; ok {
		t.Fatal("platform.multiplex should be pruned when all child platforms are gone")
	}
	if doc.RootID != "runner.default" {
		t.Fatalf("runner.default should remain as root, rootId=%q", doc.RootID)
	}
}

func TestEnvInterpolationMissingFailsWhenRootUnavailable(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.slack
platform.slack:
  use: platform/slack
  config:
    botToken: ${env:SLACK_BOT_TOKEN}
`)
	_, err := config.ResolveYAML(".", base)
	if err == nil {
		t.Fatal("expected error when root platform is unavailable")
	}
	if !strings.Contains(err.Error(), "root unavailable") {
		t.Fatalf("err=%v", err)
	}
}

func TestEnvInterpolationMissingFiltersListDeps(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    loop: loop.default
loop.default:
  use: loop/default
  deps:
    agents:
      - agent.assistant.default
      - agent.slack.default
agent.assistant.default:
  use: agent/coding
agent.slack.default:
  use: agent/coding
  config:
    token: ${env:SLACK_ONLY_AGENT_TOKEN}
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	agents := doc.Shared["loop.default"].Deps["agents"].([]any)
	if len(agents) != 1 || agents[0] != "agent.assistant.default" {
		t.Fatalf("agents=%v", agents)
	}
}

func TestEnvInterpolationOptionalEmptyKeepsInstance(t *testing.T) {
	t.Parallel()
	base := []byte(`runner.default:
  use: runner
  deps:
    platform: platform.default
platform.default:
  use: platform/cli
  config:
    apiToken: ${env:CHAT_API_TOKEN:-}
`)
	resolved, err := config.ResolveYAML(".", base)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Shared["platform.default"].Config["apiToken"] != "" {
		t.Fatalf("apiToken=%v", doc.Shared["platform.default"].Config["apiToken"])
	}
}

func TestFileInterpolationOptionalEmptyKeepsInstance(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := []byte(`runner.default:
  use: runner
  deps:
    prompt: prompt.default
prompt.default:
  use: prompt/section/static
  config:
    name: persona
    content: ${file:missing-persona.md:-}
`)
	resolved, err := config.ResolveYAML(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.FromYAML(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Shared["prompt.default"].Config["content"] != "" {
		t.Fatalf("content=%q", doc.Shared["prompt.default"].Config["content"])
	}
}
