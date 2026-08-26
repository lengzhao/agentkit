package multiplex

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
)

type idStub struct {
	id string
}

func (s idStub) PlatformID() string { return s.id }

func (s idStub) Receive(context.Context) (agentkit.MessageEvent, error) {
	panic("unused")
}

func (s idStub) Send(context.Context, agentkit.OutboundEvent) error {
	panic("unused")
}

func TestAssignPlatformIDsUsesLeafPlatformID(t *testing.T) {
	t.Parallel()

	got, err := assignPlatformIDs([]agentkit.Platform{
		idStub{id: "cli"},
		idStub{id: "timer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if _, ok := got["cli"]; !ok {
		t.Fatalf("missing cli: %#v", got)
	}
	if _, ok := got["timer"]; !ok {
		t.Fatalf("missing timer: %#v", got)
	}
}

func TestAssignPlatformIDsDisambiguatesDuplicates(t *testing.T) {
	t.Parallel()

	got, err := assignPlatformIDs([]agentkit.Platform{
		idStub{id: "timer"},
		idStub{id: "timer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if _, ok := got["timer"]; !ok {
		t.Fatalf("missing timer: %#v", got)
	}
	if _, ok := got["timer1"]; !ok {
		t.Fatalf("missing timer1: %#v", got)
	}
}

func TestAssignPlatformIDsRejectsMissingIdentifier(t *testing.T) {
	t.Parallel()

	_, err := assignPlatformIDs([]agentkit.Platform{&Platform{}})
	if err == nil {
		t.Fatal("expected error")
	}
}
