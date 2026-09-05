package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/session"
)

type stubAgent struct {
	id   agentkit.AgentID
	detail string
}

func (s stubAgent) ID() agentkit.AgentID { return s.id }

func (s stubAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }

func (s stubAgent) AgentCatalogEntry() string { return s.detail }

func TestAgentHelpCommand(t *testing.T) {
	t.Parallel()
	agents := []agentkit.Agent{
		stubAgent{id: "assistant", detail: "agent \"assistant\"\nkind: agent/coding\nmodel: gpt-5.4"},
		stubAgent{id: "reviewer", detail: "agent \"reviewer\"\nkind: agent/coding"},
	}
	cmd := agent.HelpCommand(agents)
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "list",
			args: nil,
			want: []string{"Registered agents:", "assistant", "reviewer", "Use /agent <id>", "Use /agent use <id>"},
		},
		{
			name: "detail",
			args: []string{"assistant"},
			want: []string{"agent \"assistant\"", "kind: agent/coding", "model: gpt-5.4"},
		},
		{
			name: "unknown agent",
			args: []string{"no-such-agent"},
			want: []string{"unknown agent"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := cmd.CommandExec(context.Background(), strings.Join(tc.args, " "))
			if tc.name == "unknown agent" {
				if err == nil {
					t.Fatal("expected error")
				}
				out = err.Error()
			} else if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in output:\n%s", want, out)
				}
			}
		})
	}
}

func TestAgentListShowsSessionAgent(t *testing.T) {
	t.Parallel()

	mem, err := session.NewMemory(session.MemoryConfig{ID: "cli:test"})
	if err != nil {
		t.Fatal(err)
	}
	store := session.NewStaticStore(mem)
	agents := []agentkit.Agent{
		stubAgent{id: "assistant"},
		stubAgent{id: "reviewer"},
	}
	cmd := agent.Command(agents, store, "assistant")
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:test", Workspace: "cli:test"})

	out, err := cmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Session agent: assistant (default)", "assistant *", "reviewer"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}

	if err := store.SetAgentBind(ctx, "cli:test", "reviewer"); err != nil {
		t.Fatal(err)
	}
	out, err = cmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Session agent: reviewer", "Default agent: assistant"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "reviewer  *") && !strings.Contains(out, "reviewer *") {
		t.Errorf("expected current agent marker on reviewer in output:\n%s", out)
	}
}
