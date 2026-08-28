package cli_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit"
	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/command"
	"github.com/lengzhao/agentkit/runtime/platform/cli"
	"github.com/lengzhao/agentkit/runtime/subagent"
	runtimeWorkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// captureStderr swaps the process-wide os.Stderr, so its callers must not run in
// parallel — not with each other, and not with anything else in this package
// that prints. Help tests therefore stay sequential: Go resumes the parallel
// tests in cli_test.go only after the serial pass finishes.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func testHelpCommands(t *testing.T) agentkit.Commands {
	t.Helper()
	ws, err := runtimeWorkspace.New(runtimeWorkspace.Config{})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := command.New(command.Config{}, command.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	collector, ok := reg.(agentkit.CommandCollector)
	if !ok {
		t.Fatal("expected commands registry to implement CommandCollector")
	}
	provider, ok := reg.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("expected commands registry to implement CommandProvider")
	}
	if err := collector.SetCommands([]agentkit.CommandProvider{
		provider,
		agentProvider{},
		subagentProvider{ws: ws},
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

type agentProvider struct{}

func (agentProvider) Commands() []agentkit.Command {
	return []agentkit.Command{agent.HelpCommand([]agentkit.Agent{
		stubHelpAgent{id: "assistant"},
	})}
}

type stubHelpAgent struct{ id agentkit.AgentID }

func (s stubHelpAgent) ID() agentkit.AgentID { return s.id }

func (s stubHelpAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }

type subagentProvider struct {
	ws cw.Service
}

func (p subagentProvider) Commands() []agentkit.Command {
	return []agentkit.Command{subagent.HelpCommand(p.ws, subagent.DefaultDefinitionDirs())}
}

// helpOutput drives one /help invocation through Receive and returns what the
// user would have seen.
func helpOutput(t *testing.T, prompt string) string {
	t.Helper()
	p, err := cli.New(cli.Config{Prompt: prompt}, cli.Deps{Commands: testHelpCommands(t)})
	if err != nil {
		t.Fatal(err)
	}
	return captureStderr(t, func() {
		event, err := p.Receive(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if event.Message.Role != "" {
			t.Fatalf("a slash command must not produce a message event, got %+v", event)
		}
	})
}

func TestCLIHelpCommands(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   []string
	}{
		{
			name:   "plugin list via help",
			prompt: "/help plugin -l",
			want:   []string{"Registered plugin kinds:", "llm/openai-compatible", "Use /plugin <kind>"},
		},
		{
			name:   "plugin kind via help",
			prompt: "/help plugin llm/openai-compatible",
			want:   []string{"go doc github.com/lengzhao/agentkit/runtime/llm.NewOpenAI", "llm/openai-compatible"},
		},
		{
			name:   "plugin direct",
			prompt: "/plugin -l",
			want:   []string{"Registered plugin kinds:", "Use /plugin <kind>"},
		},
		{
			name:   "agent list via help",
			prompt: "/help agent",
			want:   []string{"Registered agents:", "assistant", "Use /agent <id>"},
		},
		{
			name:   "agent detail via help",
			prompt: "/help agent assistant",
			want:   []string{"agent \"assistant\""},
		},
		{
			name:   "subagent list via help",
			prompt: "/help subagent",
			want:   []string{"Registered subagents:", "Use /subagent <name>"},
		},
		{
			name:   "subagent plugin fallback via help",
			prompt: "/help subagent inprocess",
			want:   []string{"go doc github.com/lengzhao/agentkit/runtime/subagent.New", "subagent/inprocess"},
		},
		{
			name:   "unknown topic",
			prompt: "/help nonsense",
			want:   []string{"unknown help topic"},
		},
		{
			name:   "commands",
			prompt: "/help",
			want:   []string{"Commands:", "/plugin", "/agent", "/subagent", "/exit, /quit"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := helpOutput(t, tc.prompt)
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in output:\n%s", want, out)
				}
			}
		})
	}
}
