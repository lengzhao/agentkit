package subagent_test

import (
	"context"
	"strings"
	"testing"

	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/subagent"
	runtimeWorkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestSubagentHelpCommand(t *testing.T) {
	t.Parallel()
	ws, err := runtimeWorkspace.New(runtimeWorkspace.Config{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := subagent.HelpCommand(ws, subagent.DefaultDefinitionDirs())
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "list",
			args: nil,
			want: []string{"Registered subagents:", "Use /subagent <name>"},
		},
		{
			name: "plugin kind fallback",
			args: []string{"inprocess"},
			want: []string{"go doc github.com/lengzhao/agentkit/runtime/subagent.New", "subagent/inprocess"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := cmd.CommandExec(context.Background(), tc.args...)
			if err != nil {
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
