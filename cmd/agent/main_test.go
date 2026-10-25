package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
	"github.com/lengzhao/pluginkit/build"
)

type failingLifecycleRunner struct{}

func (*failingLifecycleRunner) Run(context.Context, *build.Result) error {
	return errors.New("lifecycle test failure")
}
func (*failingLifecycleRunner) Stop(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return os.WriteFile(os.Getenv("AGENTKIT_TEST_STOP_MARKER"), []byte("stopped"), 0600)
}

func TestMainCleansUpBeforeFatalRunError(t *testing.T) {
	if os.Getenv("AGENTKIT_TEST_FATAL_RUN") == "1" {
		pluginkit.Register("runner/lifecycle-test", func(struct{}, struct{}) (agentkit.Runner, error) { return &failingLifecycleRunner{}, nil })
		os.Args = []string{"agent", "-base", os.Getenv("AGENTKIT_TEST_BASE"), "-config", ""}
		main()
		return
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	marker := filepath.Join(dir, "stopped")
	if err := os.WriteFile(configPath, []byte("runner.default:\n  use: runner/lifecycle-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainCleansUpBeforeFatalRunError$")
	cmd.Env = append(os.Environ(), "AGENTKIT_TEST_FATAL_RUN=1", "AGENTKIT_TEST_BASE="+configPath, "AGENTKIT_TEST_STOP_MARKER="+marker)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("exit = %v; output: %s", err, output)
	}
	if !strings.Contains(string(output), "lifecycle test failure") {
		t.Fatalf("unexpected error: %s", output)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("Stop was skipped before fatal exit: %v; output: %s", err, output)
	}
}
