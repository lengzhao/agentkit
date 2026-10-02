package agentkit_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/config"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/runner"
	"github.com/lengzhao/pluginkit/build"
)

// stubCredentialEnv makes config-build tests hermetic: every env credential
// referenced by config.base.yaml gets a dummy value so the build does not
// depend on the developer's local environment.
func stubCredentialEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OPENAI_API_KEY",
		"DEEPSEEK_API_KEY",
		"EXA_API_KEY",
		"TAVILY_API_KEY",
		"SLACK_APP_TOKEN",
		"SLACK_BOT_TOKEN",
	} {
		t.Setenv(key, "sk-test")
	}
}

func TestConfigBaseLoadsAndBuilds(t *testing.T) {
	stubCredentialEnv(t)
	doc, err := config.LoadDocument(config.DefaultBasePath, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.RootID != "runner.default" {
		t.Fatalf("rootId=%q", doc.RootID)
	}
	if doc.Plugin.Use != "runner" {
		t.Fatalf("root use=%q", doc.Plugin.Use)
	}

	graph := doc.ToGraph()
	if _, ok := graph["runner.default"]; !ok {
		t.Fatal("graph missing runner.default")
	}

	_, _, err = build.Build[agentkit.Runner](context.Background(), graph, doc.RootID)
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
}

func TestPresetCodingOverlayBuilds(t *testing.T) {
	stubCredentialEnv(t)
	doc, err := config.LoadDocument(config.DefaultBasePath, "presets/coding.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if doc.RootID != "runner.default" {
		t.Fatalf("rootId=%q", doc.RootID)
	}

	workspace, ok := doc.Shared["workspace.default"]
	if !ok {
		t.Fatal("missing workspace.default")
	}
	if workspace.Config["scope"] != "local" {
		t.Fatalf("workspace.scope=%v", workspace.Config["scope"])
	}

	_, _, err = build.Build[agentkit.Runner](context.Background(), doc.ToGraph(), doc.RootID)
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
}

func TestPresetChatAPIOverlayBuilds(t *testing.T) {
	stubCredentialEnv(t)
	doc, err := config.LoadDocument(config.DefaultBasePath, "presets/chat-api.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if doc.RootID != "runner.default" {
		t.Fatalf("rootId=%q", doc.RootID)
	}

	runnerInst, _, err := build.Build[agentkit.Runner](context.Background(), doc.ToGraph(), doc.RootID)
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
	root, ok := runnerInst.(*runner.Root)
	if !ok {
		t.Fatalf("runner type = %T, want *runner.Root", runnerInst)
	}
	if root.SessionStore() == nil {
		t.Fatal("chat-api preset runner must wire sessionStore for /new active mapping")
	}
}

func TestPresetCodingSmokeOverlayBuilds(t *testing.T) {
	stubCredentialEnv(t)
	doc, err := config.LoadDocument(config.DefaultBasePath, "presets/coding-smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if doc.RootID != "runner.default" {
		t.Fatalf("rootId=%q", doc.RootID)
	}

	_, _, err = build.Build[agentkit.Runner](context.Background(), doc.ToGraph(), doc.RootID)
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
}
