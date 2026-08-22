package cli_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/lengzhao/agentkit/runtime/platform/cli"
)

func TestCLIExitCommand(t *testing.T) {
	t.Parallel()
	p, err := cli.New(cli.Config{Prompt: "/exit"}, cli.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Receive(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF on /exit, got %v", err)
	}
}

func TestCLIHelpDoesNotDispatch(t *testing.T) {
	t.Parallel()
	p, err := cli.New(cli.Config{Prompt: "/help"}, cli.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	event, err := p.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.Message.Role != "" {
		t.Fatalf("expected empty event for /help, got %+v", event)
	}
}
