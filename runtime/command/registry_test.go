package command

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/cap/command"
)

type stubHandler struct {
	name string
}

func (s stubHandler) Descriptor() command.Descriptor {
	return command.Descriptor{Name: s.name, Description: "stub"}
}

func (s stubHandler) Handle(_ context.Context, _ command.Request) (command.Result, error) {
	return command.Result{}, nil
}

func TestRegistryDispatch(t *testing.T) {
	t.Parallel()
	r, err := New(Config{}, Deps{Handlers: []command.Handler{
		stubHandler{name: "ping"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Dispatch(context.Background(), command.Request{Name: "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Handled {
		t.Fatal("expected handled result")
	}
	result, err = r.Dispatch(context.Background(), command.Request{Name: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Handled {
		t.Fatal("expected unhandled result")
	}
}

func TestRegistryDuplicateName(t *testing.T) {
	t.Parallel()
	r := &Registry{byName: make(map[string]command.Handler)}
	if err := r.Register(command.Descriptor{Name: "ping"}, stubHandler{name: "ping"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(command.Descriptor{Name: "ping"}, stubHandler{name: "ping"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestRegistryListSorted(t *testing.T) {
	t.Parallel()
	r, err := New(Config{}, Deps{Handlers: []command.Handler{
		stubHandler{name: "zeta"},
		stubHandler{name: "alpha"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	list := r.List()
	if len(list) != 2 || list[0].Name != "alpha" || list[1].Name != "zeta" {
		t.Fatalf("unexpected list order: %+v", list)
	}
}
