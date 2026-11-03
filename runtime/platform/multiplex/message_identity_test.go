package multiplex_test

import (
	"context"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/platform/multiplex"
	"testing"
)

type identifiedStub struct {
	stubPlatform
	common.InstanceIdentity
}

func TestMultiplexPreservesAssignedMessageIdentities(t *testing.T) {
	a := &identifiedStub{stubPlatform: stubPlatform{id: "slack"}}
	a.SetPlatformInstance("work")
	b := &identifiedStub{stubPlatform: stubPlatform{id: "slack"}}
	b.SetPlatformInstance("home")
	m, err := multiplex.New(multiplex.Config{}, multiplex.Deps{Platforms: []agentkit.Platform{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if a.PlatformInstance() != "work" || b.PlatformInstance() != "home" {
		t.Fatalf("identities overwritten: %s %s", a.PlatformInstance(), b.PlatformInstance())
	}
	if err = m.Send(context.Background(), agentkit.OutboundEvent{Route: agentkit.RouteRef{Instance: "home"}}); err != nil || len(b.sent) != 1 || len(a.sent) != 0 {
		t.Fatalf("route failed: %v", err)
	}
	b.SetPlatformInstance("work")
	if _, err = multiplex.New(multiplex.Config{}, multiplex.Deps{Platforms: []agentkit.Platform{a, b}}); err == nil {
		t.Fatal("duplicate explicit identity accepted")
	}
}

func TestMultiplexConfiguredMessageIdentities(t *testing.T) {
	a := &identifiedStub{stubPlatform: stubPlatform{id: "slack"}}
	b := &identifiedStub{stubPlatform: stubPlatform{id: "slack"}}
	cfg := multiplex.Config{Instances: []string{"work", "home"}}
	if _, err := multiplex.New(cfg, multiplex.Deps{Platforms: []agentkit.Platform{a, b}}); err != nil {
		t.Fatal(err)
	}
	if a.PlatformInstance() != "work" || b.PlatformInstance() != "home" {
		t.Fatalf("%s %s", a.PlatformInstance(), b.PlatformInstance())
	}
}
