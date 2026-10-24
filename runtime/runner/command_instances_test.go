package runner

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/command"
	"github.com/lengzhao/pluginkit/build"
)

type instanceTestProvider struct{ value string }

func (p instanceTestProvider) Commands() []agentkit.Command { return []agentkit.Command{p} }
func (p instanceTestProvider) Name() string                 { return "ping" }
func (p instanceTestProvider) Alias() string                { return "p" }
func (p instanceTestProvider) Description() string          { return "test" }
func (p instanceTestProvider) CommandExec(context.Context, string) (string, error) {
	return p.value, nil
}

func TestAttachCommandsPreservesInstanceIDs(t *testing.T) {
	registry, err := command.NewFromProviders(command.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := &build.Result{Instances: []build.Instance{
		{ID: "one", Value: instanceTestProvider{"first"}},
		{ID: "two", Value: instanceTestProvider{"second"}},
		{ID: "commands", Value: registry},
	}}
	if err := attachCommands(result); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"ping_one": "first", "p_one": "first", "ping_two": "second", "p_two": "second"} {
		got, err := registry.Dispatch(context.Background(), name, "")
		if err != nil || got != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
}
