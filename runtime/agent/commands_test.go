package agent_test

import (
	"context"
	"strings"
	"testing"

	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/agent"
)

func TestAgentHelpCommand(t *testing.T) {
	t.Parallel()
	cmd := agent.HelpCommand()
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "list",
			args: nil,
			want: []string{"Registered agent kinds:", "agent/coding", "Use /agent <name>"},
		},
		{
			name: "kind",
			args: []string{"coding"},
			want: []string{"go doc github.com/lengzhao/agentkit/runtime/agent.New", "agent/coding"},
		},
		{
			name: "unknown kind",
			args: []string{"no/such-kind"},
			want: []string{"unknown agent kind"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := cmd.CommandExec(context.Background(), tc.args...)
			if tc.name == "unknown kind" {
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
