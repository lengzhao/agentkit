package presettest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	"github.com/lengzhao/pluginkit/build"
)

func TestLoadIsolatesPersistedRuntime(t *testing.T) {
	first := Load(t, "presets/autonomous-smoke.yaml")
	second := Load(t, "presets/autonomous-smoke.yaml")
	firstRoot, _ := first.Shared["workspace.default"].Config["global"].(string)
	secondRoot, _ := second.Shared["workspace.default"].Config["global"].(string)
	if !filepath.IsAbs(firstRoot) || firstRoot == secondRoot {
		t.Fatalf("preset loads must have distinct temporary workspace roots, got %q and %q", firstRoot, secondRoot)
	}
	ctx := context.Background()
	firstWS, _, err := build.Build[workspace.Service](ctx, first.ToGraph(), "workspace.default")
	if err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalAgentBind(ctx, firstWS, "unconfigured-agent"); err != nil {
		t.Fatal(err)
	}
	secondWS, _, err := build.Build[workspace.Service](ctx, second.ToGraph(), "workspace.default")
	if err != nil {
		t.Fatal(err)
	}
	id, err := sessbind.GlobalAgentBind(ctx, secondWS)
	if err != nil || id != "" {
		t.Fatalf("another load inherited agent %q: %v", id, err)
	}
}
