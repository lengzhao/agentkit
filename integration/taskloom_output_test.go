//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/agentkit/testing/presettest"
	"github.com/lengzhao/pluginkit/build"
)

func TestIntegrationTaskloomOutputReaderAvailableWithoutFilesystemTools(t *testing.T) {
	doc := presettest.Load(t, "presets/taskloom.yaml")
	tools, _, err := build.Build[agentkit.ToolRuntime](context.Background(), doc.ToGraph(), "tools.taskloom")
	if err != nil {
		t.Fatal(err)
	}
	visible, err := tools.Visible(agenttest.TurnContext("test:taskloom-output", "taskloom"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, spec := range visible {
		if spec.Name == "read_tool_output" {
			found = true
		}
		if spec.Name == "read" || spec.Name == "grep" || spec.Name == "bash" {
			t.Fatalf("unexpected filesystem tool: %s", spec.Name)
		}
	}
	if !found {
		t.Fatal("Taskloom preset cannot read truncated tool outputs")
	}
}
