package runner_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/lengzhao/agentkit/runtime/runner"
)

func TestCLIExitCommand(t *testing.T) {
	t.Parallel()
	cli, err := runner.NewCLI(runner.CLIConfig{Prompt: "/exit"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cli.Receive(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF on /exit, got %v", err)
	}
}

func TestCLIHelpDoesNotDispatch(t *testing.T) {
	t.Parallel()
	cli, err := runner.NewCLI(runner.CLIConfig{Prompt: "/help"})
	if err != nil {
		t.Fatal(err)
	}
	event, err := cli.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.Message.Role != "" {
		t.Fatalf("expected empty event for /help, got %+v", event)
	}
}
