package slack_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
)

func TestNewResolvesTokenRefs(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test-bot")
	t.Setenv("SLACK_APP_TOKEN", "xapp-test-app")

	graph := map[string]any{
		"credentials": map[string]any{"use": "credentials/env"},
		"slack": map[string]any{
			"use": "platform/slack",
			"config": map[string]any{
				"botTokenRef": "env:SLACK_BOT_TOKEN",
				"appTokenRef": "env:SLACK_APP_TOKEN",
			},
			"deps": map[string]any{
				"credentials": "credentials",
				"workspace": map[string]any{
					"use":    "workspace/default",
					"config": map[string]any{"root": t.TempDir()},
				},
			},
		},
	}

	_, _, err := build.Build[agentkit.Platform](context.Background(), graph, "slack")
	if err != nil {
		t.Fatalf("build slack: %v", err)
	}
}
