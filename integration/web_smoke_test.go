//go:build integration

package integration_test

import (
	"testing"

	"github.com/lengzhao/agentkit/testing/presettest"
)

func TestIntegrationWebSmokePresetBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("integration preset build")
	}
	doc := presettest.Load(t, "presets/web.yaml", "presets/web-smoke.yaml")
	presettest.MustBuildRunner(t, doc)
}
