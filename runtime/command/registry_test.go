package command

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
)

type stubCommand struct {
	name string
}

func (s stubCommand) Name() string        { return s.name }
func (s stubCommand) Alias() string       { return "" }
func (s stubCommand) Description() string { return "stub" }
func (s stubCommand) CommandExec(context.Context, ...string) (string, error) {
	return "", nil
}

type stubAliasCommand struct {
	name  string
	alias string
}

func (s stubAliasCommand) Name() string        { return s.name }
func (s stubAliasCommand) Alias() string       { return s.alias }
func (s stubAliasCommand) Description() string { return "stub" }
func (s stubAliasCommand) CommandExec(context.Context, ...string) (string, error) {
	return "", nil
}

type stubProvider struct {
	commands []agentkit.Command
}

func (p stubProvider) Commands() []agentkit.Command { return p.commands }

func TestRegistryDispatch(t *testing.T) {
	t.Parallel()
	r, err := NewFromProviders(Config{}, []agentkit.CommandProvider{
		stubProvider{commands: []agentkit.Command{stubCommand{name: "ping"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Dispatch(context.Background(), "ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected handled result")
	}
	result, err = r.Dispatch(context.Background(), "missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected unhandled result")
	}
}

func TestRegistryDuplicateName(t *testing.T) {
	t.Parallel()
	r := &Registry{byName: make(map[string]agentkit.Command)}
	if err := r.register(stubCommand{name: "ping"}); err != nil {
		t.Fatal(err)
	}
	if err := r.register(stubCommand{name: "ping"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestRegistryListSorted(t *testing.T) {
	t.Parallel()
	r, err := NewFromProviders(Config{}, []agentkit.CommandProvider{
		stubProvider{commands: []agentkit.Command{
			stubCommand{name: "zeta"},
			stubCommand{name: "alpha"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	list := r.List()
	if len(list) != 2 || list[0].Name() != "alpha" || list[1].Name() != "zeta" {
		t.Fatalf("unexpected list order: %+v", list)
	}
}

func TestRegistryDenyList(t *testing.T) {
	t.Parallel()
	r, err := NewFromProviders(Config{Deny: []string{"compact"}}, []agentkit.CommandProvider{
		stubProvider{commands: []agentkit.Command{
			stubCommand{name: "compact"},
			stubCommand{name: "new"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.List()) != 1 || r.List()[0].Name() != "new" {
		t.Fatalf("unexpected list after deny: %+v", r.List())
	}
}

func TestRegistryAllowList(t *testing.T) {
	t.Parallel()
	r, err := NewFromProviders(Config{Allow: []string{"sess"}}, []agentkit.CommandProvider{
		stubProvider{commands: []agentkit.Command{
			stubCommand{name: "new"},
			stubAliasCommand{name: "session", alias: "sess"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.List()) != 1 || r.List()[0].Name() != "session" {
		t.Fatalf("unexpected list after allow: %+v", r.List())
	}
}
