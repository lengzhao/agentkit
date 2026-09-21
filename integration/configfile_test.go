//go:build integration

package integration_test

import (
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// integrationFS builds an unrestricted filesystem/local over a static root.
func integrationFS(t *testing.T, root string) filesystem.Service {
	t.Helper()
	return integrationFSOver(t, rtworkspace.Static(root))
}

// integrationFSOver builds an unrestricted filesystem/local delegating to ws.
func integrationFSOver(t *testing.T, ws workspace.Service) filesystem.Service {
	t.Helper()
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: ".", Unrestricted: true}, rtfilesystem.Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func integrationTelemetry(t *testing.T) captelemetry.Toolkit {
	t.Helper()
	tk, err := rttelemetry.NewToolkit(struct{}{}, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}
