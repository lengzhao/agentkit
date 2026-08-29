package common

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

type stubCommand struct {
	name string
	out  string
}

func (c stubCommand) Name() string        { return c.name }
func (c stubCommand) Alias() string       { return "" }
func (c stubCommand) Description() string { return "stub" }
func (c stubCommand) CommandExec(context.Context, ...string) (string, error) {
	return c.out, nil
}

type stubCommands struct {
	byName map[string]agentkit.Command
}

func (s stubCommands) Dispatch(_ context.Context, name string, _ []string) (string, error) {
	cmd, ok := s.byName[name]
	if !ok {
		return "", agentkit.ErrCommandNotHandled
	}
	return cmd.CommandExec(context.Background())
}

func (s stubCommands) List() []agentkit.Command {
	out := make([]agentkit.Command, 0, len(s.byName))
	for _, cmd := range s.byName {
		out = append(out, cmd)
	}
	return out
}

func TestParseSlashCommand(t *testing.T) {
	name, args, ok := ParseSlashCommand("/help topic")
	if !ok || name != "help" || args != "topic" {
		t.Fatalf("got name=%q args=%q ok=%v", name, args, ok)
	}
	if _, _, ok := ParseSlashCommand("hello"); ok {
		t.Fatal("expected non-slash")
	}
}

func TestProcessSlashHelp(t *testing.T) {
	out, err := ProcessSlash(context.Background(), nil, "slack:C:U", "/help")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled || !strings.Contains(out.Reply, "/help") {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

func TestFormatHelpMultiline(t *testing.T) {
	cmds := stubCommands{byName: map[string]agentkit.Command{
		"ping": stubCommand{name: "ping", out: "pong"},
	}}
	text := FormatHelp(cmds)
	if !strings.Contains(text, "可用命令:\n") {
		t.Fatalf("expected header newline, got %q", text)
	}
	if strings.Count(text, "\n") < 2 {
		t.Fatalf("expected one command per line, got %q", text)
	}
	if !strings.Contains(text, "/ping") {
		t.Fatalf("missing registered command: %q", text)
	}
}

func TestProcessSlashDispatch(t *testing.T) {
	cmds := stubCommands{byName: map[string]agentkit.Command{
		"ping": stubCommand{name: "ping", out: "pong"},
	}}
	out, err := ProcessSlash(context.Background(), cmds, "slack:C:U", "/ping")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled || out.Reply != "pong" {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

func TestProcessSlashUnknownForwards(t *testing.T) {
	out, err := ProcessSlash(context.Background(), stubCommands{byName: nil}, "slack:C:U", "/missing")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashForward || !strings.Contains(out.Reply, "转发给 Agent") {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}
