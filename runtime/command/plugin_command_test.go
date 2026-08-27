package command_test

import (
	"context"
	"strings"
	"testing"

	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/command"
)

func TestPluginHelpCommand(t *testing.T) {
	t.Parallel()
	cmd := command.PluginHelpCommand()
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "list",
			args: []string{"-l"},
			want: []string{"Registered plugin kinds:", "llm/openai-compatible", "Use /plugin <kind>"},
		},
		{
			name: "kind",
			args: []string{"llm/openai-compatible"},
			want: []string{"go doc github.com/lengzhao/agentkit/runtime/llm.NewOpenAI", "llm/openai-compatible"},
		},
		{
			name: "unknown kind",
			args: []string{"no/such-kind"},
			want: []string{"unknown plugin kind"},
		},
		{
			name: "missing kind",
			args: nil,
			want: []string{"usage: /plugin -l"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := cmd.CommandExec(context.Background(), tc.args...)
			if tc.name == "missing kind" || tc.name == "unknown kind" {
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
