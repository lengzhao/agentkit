package command

import (
	"context"
	"errors"
	"strings"
	"testing"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit")

type stubCommand struct {
	name string
}

func (s stubCommand) Name() string        { return s.name }
func (s stubCommand) Alias() string       { return "" }
func (s stubCommand) Description() string { return "stub" }
func (s stubCommand) CommandExec(context.Context, string) (string, error) {
	return "", nil
}

type stubAliasCommand struct {
	name  string
	alias string
}

func (s stubAliasCommand) Name() string        { return s.name }
func (s stubAliasCommand) Alias() string       { return s.alias }
func (s stubAliasCommand) Description() string { return "stub" }
func (s stubAliasCommand) CommandExec(context.Context, string) (string, error) {
	return "", nil
}

type stubProvider struct {
	commands []agentkit.Command
}

func (p stubProvider) Commands() []agentkit.Command { return p.commands }

type rawArgsCommand struct {
	name string
	got  *string
}

func (c rawArgsCommand) Name() string        { return c.name }
func (c rawArgsCommand) Alias() string       { return "" }
func (c rawArgsCommand) Description() string { return "raw" }
func (c rawArgsCommand) CommandExec(_ context.Context, args string) (string, error) {
	if c.got != nil {
		*c.got = args
	}
	return "ok", nil
}

func TestRegistryDispatchRawArgs(t *testing.T) {
	t.Parallel()
	var got string
	r, err := NewFromProviders(Config{}, []agentkit.CommandProvider{
		stubProvider{commands: []agentkit.Command{rawArgsCommand{name: "shell", got: &got}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Dispatch(context.Background(), "shell", `echo "hello world"`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" || got != `echo "hello world"` {
		t.Fatalf("got arg %q out %q", got, out)
	}
}

func TestRegistryDispatchFields(t *testing.T) {
	t.Parallel()
	var got string
	r, err := NewFromProviders(Config{}, []agentkit.CommandProvider{
		stubProvider{commands: []agentkit.Command{stubCaptureCommand{got: &got}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(context.Background(), "ping", "one two"); err != nil {
		t.Fatal(err)
	}
	if got != "one,two" {
		t.Fatalf("got args %q", got)
	}
}

type stubCaptureCommand struct {
	got *string
}

func (stubCaptureCommand) Name() string        { return "ping" }
func (stubCaptureCommand) Alias() string       { return "" }
func (stubCaptureCommand) Description() string { return "capture" }
func (c stubCaptureCommand) CommandExec(_ context.Context, args string) (string, error) {
	if c.got != nil {
		*c.got = strings.Join(strings.Fields(strings.TrimSpace(args)), ",")
	}
	return "", nil
}

func TestRegistryDispatch(t *testing.T) {
	t.Parallel()
	r, err := NewFromProviders(Config{}, []agentkit.CommandProvider{
		stubProvider{commands: []agentkit.Command{stubCommand{name: "ping"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Dispatch(context.Background(), "ping", "")
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		t.Fatalf("unexpected output: %q", result)
	}
	_, err = r.Dispatch(context.Background(), "missing", "")
	if !errors.Is(err, agentkit.ErrCommandNotHandled) {
		t.Fatalf("missing command err = %v", err)
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

type adminOnlyCommand struct {
	name string
}

func (c adminOnlyCommand) Name() string        { return c.name }
func (adminOnlyCommand) Alias() string         { return "" }
func (adminOnlyCommand) Description() string   { return "admin" }
func (adminOnlyCommand) CommandExec(context.Context, string) (string, error) {
	return "secret", nil
}

func TestRegistryAdminOnlyRequiresAdmin(t *testing.T) {
	t.Parallel()
	r, err := NewFromProviders(Config{
		Admins:    []string{"U1"},
		AdminOnly: []string{"shell"},
	}, []agentkit.CommandProvider{
		stubProvider{commands: []agentkit.Command{adminOnlyCommand{name: "shell"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Actor: agentkit.ActorRef{UserID: "U2"}})
	_, err = r.Dispatch(ctx, "shell", "")
	if !errors.Is(err, agentkit.ErrCommandForbidden) {
		t.Fatalf("non-admin err = %v", err)
	}
	ctx = session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Actor: agentkit.ActorRef{UserID: "U1"}})
	out, err := r.Dispatch(ctx, "shell", "")
	if err != nil || out != "secret" {
		t.Fatalf("admin dispatch = %q err %v", out, err)
	}
}

func TestRegistryAdminOnlySkippedWhenAdminsUnset(t *testing.T) {
	t.Parallel()
	r, err := NewFromProviders(Config{AdminOnly: []string{"shell"}}, []agentkit.CommandProvider{
		stubProvider{commands: []agentkit.Command{adminOnlyCommand{name: "shell"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Dispatch(context.Background(), "shell", "")
	if err != nil || out != "secret" {
		t.Fatalf("dispatch = %q err %v", out, err)
	}
}

func TestRegistryEnrichSlashContext(t *testing.T) {
	t.Parallel()
	r, err := NewFromProviders(Config{Admins: []string{"U1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Actor: agentkit.ActorRef{UserID: "U1"}})
	ctx = r.EnrichSlashContext(ctx)
	if !agentkit.IsAdmin(ctx) {
		t.Fatal("expected admin ctx")
	}
}
