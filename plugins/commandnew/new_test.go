package commandnew_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/command"
	"github.com/lengzhao/agentkit/plugins/commandnew"
)

func TestNewCommandCreatesSession(t *testing.T) {
	t.Parallel()
	handler, err := commandnew.NewHandler(commandnew.Config{}, commandnew.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	result, err := handler.Handle(context.Background(), command.Request{
		Name:      "new",
		SessionID: agentkit.SessionID("cli:default"),
		ErrOut:    &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.NewSession == "" || !strings.HasPrefix(string(result.NewSession), "cli:") {
		t.Fatalf("unexpected session id: %q", result.NewSession)
	}
	if !strings.Contains(out.String(), "new session:") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}
