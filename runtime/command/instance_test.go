package command

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

type instanceProvider struct {
	stubProvider
	id string
}

func (p instanceProvider) CommandInstanceID() string { return p.id }
func instance(id string, commands ...agentkit.Command) agentkit.CommandProvider {
	return instanceProvider{stubProvider{commands}, id}
}

func TestInstanceCommandNames(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		providers := []agentkit.CommandProvider{
			instance("local", stubAliasCommand{"shell", "sh"}, stubCommand{"unique"}),
			instance("remote", stubAliasCommand{"shell", "sh"}),
		}
		if reverse {
			providers[0], providers[1] = providers[1], providers[0]
		}
		r, err := NewFromProviders(Config{}, providers)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, cmd := range r.List(context.Background()) {
			names = append(names, cmd.Name())
		}
		if !reflect.DeepEqual(names, []string{"shell_local", "shell_remote", "unique"}) {
			t.Fatalf("names = %v", names)
		}
		for _, key := range []string{"shell_local", "shell_remote", "sh_local", "sh_remote", "unique"} {
			if _, err := r.Dispatch(context.Background(), key, ""); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
		}
		if _, err := r.Dispatch(context.Background(), "shell", ""); err == nil || !strings.Contains(err.Error(), "shell_local") || !strings.Contains(err.Error(), "shell_remote") {
			t.Fatalf("ambiguity = %v", err)
		}
	}
}

func TestInstanceAliasCollisionOnlyRenamesAlias(t *testing.T) {
	r, err := NewFromProviders(Config{}, []agentkit.CommandProvider{
		instance("a", stubAliasCommand{"first", "shared"}),
		instance("b", stubAliasCommand{"second", "shared"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	list := r.List(context.Background())
	if list[0].Name() != "first" || list[0].Alias() != "shared_a" || list[1].Name() != "second" || list[1].Alias() != "shared_b" {
		t.Fatalf("commands = %#v", list)
	}
}

func TestInstancePolicies(t *testing.T) {
	providers := []agentkit.CommandProvider{instance("a", adminOnlyCommand{"shell"}), instance("b", adminOnlyCommand{"shell"})}
	for _, selector := range []string{"shell", "shell_a"} {
		r, err := NewFromProviders(Config{Admins: []string{"admin"}, AdminOnly: []string{selector}}, providers)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Dispatch(context.Background(), "shell_a", ""); !errors.Is(err, agentkit.ErrCommandForbidden) {
			t.Fatalf("%s: %v", selector, err)
		}
	}
	for _, tc := range []struct {
		cfg  Config
		want []string
	}{
		{Config{Allow: []string{"shell_a"}}, []string{"shell_a"}},
		{Config{Deny: []string{"shell_a"}}, []string{"shell_b"}},
		{Config{Deny: []string{"shell"}}, nil},
	} {
		r, err := NewFromProviders(tc.cfg, providers)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, cmd := range r.List(context.Background()) {
			names = append(names, cmd.Name())
		}
		if !reflect.DeepEqual(names, tc.want) {
			t.Fatalf("names %v want %v", names, tc.want)
		}
	}
}

func TestInstanceUnresolvableCollision(t *testing.T) {
	for _, providers := range [][]agentkit.CommandProvider{
		{instance("a", stubCommand{"shell"}), instance("a", stubCommand{"shell"})},
		{instance("a", stubCommand{"shell"}), instance("b", stubCommand{"shell"}), instance("c", stubCommand{"shell_a"})},
		{instance("", stubCommand{"shell"}), instance("b", stubCommand{"shell"})},
	} {
		if _, err := NewFromProviders(Config{}, providers); err == nil {
			t.Fatal("expected unresolved collision error")
		}
	}
}

func TestInstanceSanitizerPreserved(t *testing.T) {
	r, err := NewFromProviders(Config{}, []agentkit.CommandProvider{
		instance("a", sanitizerStub{stubCommand{"custom"}}),
		instance("b", stubCommand{"custom"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sanitizeArgsForLog(r.byName["custom_a"], "sensitive"); got != agentkit.SlashLogRedacted {
		t.Fatalf("sanitized = %q", got)
	}
}

func TestInstanceNameAliasCollision(t *testing.T) {
	r, err := NewFromProviders(Config{}, []agentkit.CommandProvider{
		instance("a", stubAliasCommand{"first", "shared"}),
		instance("b", stubCommand{"shared"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "shared_a", "shared_b"} {
		if _, err := r.Dispatch(context.Background(), name, ""); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestInstanceUniqueNameAndAliasUnchanged(t *testing.T) {
	r, err := NewFromProviders(Config{}, []agentkit.CommandProvider{instance("a", stubAliasCommand{"shell", "sh"})})
	if err != nil {
		t.Fatal(err)
	}
	cmd := r.List(context.Background())[0]
	if cmd.Name() != "shell" || cmd.Alias() != "sh" {
		t.Fatalf("got %s %s", cmd.Name(), cmd.Alias())
	}
}

func TestInstancePoliciesDenyOverridesAllow(t *testing.T) {
	r, err := NewFromProviders(Config{Allow: []string{"shell"}, Deny: []string{"shell_a"}}, []agentkit.CommandProvider{
		instance("a", stubCommand{"shell"}), instance("b", stubCommand{"shell"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	list := r.List(context.Background())
	if len(list) != 1 || list[0].Name() != "shell_b" {
		t.Fatalf("commands = %v", list)
	}
}

func TestInstanceExcludedConflictsDoNotRequireIDs(t *testing.T) {
	for _, cfg := range []Config{{Deny: []string{"shell"}}, {Allow: []string{"unique"}}} {
		r, err := NewFromProviders(cfg, []agentkit.CommandProvider{
			stubProvider{[]agentkit.Command{stubCommand{"shell"}, stubCommand{"unique"}}},
			stubProvider{[]agentkit.Command{stubCommand{"shell"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		list := r.List(context.Background())
		if len(list) != 1 || list[0].Name() != "unique" {
			t.Fatalf("commands = %v", list)
		}
	}
}

func TestInstanceUnusableSuffixRejected(t *testing.T) {
	for _, id := range []string{"local@host", "local/host", "local\\host", "local host"} {
		if _, err := NewFromProviders(Config{}, []agentkit.CommandProvider{
			instance(id, stubCommand{"shell"}), instance("remote", stubCommand{"shell"}),
		}); err == nil {
			t.Fatalf("expected invalid suffix error for %q", id)
		}
	}
}
