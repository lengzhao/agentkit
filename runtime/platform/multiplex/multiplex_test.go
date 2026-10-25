package multiplex_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/platform/multiplex"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type stubPlatform struct {
	id      string
	receive []agentkit.MessageEvent
	recvErr []error
	sent    []agentkit.OutboundEvent
}

func (s *stubPlatform) PlatformID() string { return s.id }

func (s *stubPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	if len(s.receive) == 0 {
		return agentkit.MessageEvent{}, io.EOF
	}
	event := s.receive[0]
	s.receive = s.receive[1:]
	var err error
	if len(s.recvErr) > 0 {
		err = s.recvErr[0]
		s.recvErr = s.recvErr[1:]
	}
	return event, err
}

func (s *stubPlatform) Send(_ context.Context, out agentkit.OutboundEvent) error {
	s.sent = append(s.sent, out)
	return nil
}

func TestMultiplexPermissionCapabilityForwardsLeaf(t *testing.T) {
	t.Parallel()

	cli := &capableStub{
		stubPlatform: stubPlatform{id: "cli"},
		cap:          permission.Capability{Interactive: true},
	}
	headless := &capableStub{
		stubPlatform: stubPlatform{id: "headless"},
		cap:          permission.Capability{Interactive: false},
	}
	m, err := multiplex.New(multiplex.Config{}, multiplex.Deps{
		Platforms: []agentkit.Platform{cli, headless},
	})
	if err != nil {
		t.Fatal(err)
	}
	mp := m.(*multiplex.Platform)

	got := mp.PermissionCapabilityFor("cli")
	if !got.Interactive {
		t.Fatalf("cli cap = %+v", got)
	}
	got = mp.PermissionCapabilityFor("headless")
	if got.Interactive {
		t.Fatalf("headless cap = %+v", got)
	}
}

type capableStub struct {
	stubPlatform
	cap permission.Capability
}

func (s *capableStub) PermissionCapability() permission.Capability {
	return s.cap
}

func TestMultiplexRejectsEmptyPlatformID(t *testing.T) {
	t.Parallel()

	cli := &stubPlatform{id: "cli"}
	slack := &stubPlatform{id: "slack"}
	m, err := multiplex.New(multiplex.Config{}, multiplex.Deps{
		Platforms: []agentkit.Platform{cli, slack},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = m.Send(context.Background(), agentkit.OutboundEvent{
		Type: agentkit.EventMessageEnd,
	})
	if !errors.Is(err, agentkit.ErrOutboundRouteRequired) {
		t.Fatalf("Send() err = %v, want ErrOutboundRouteRequired", err)
	}
	if len(cli.sent) != 0 || len(slack.sent) != 0 {
		t.Fatalf("unexpected broadcast: cli=%d slack=%d", len(cli.sent), len(slack.sent))
	}
}

func TestMultiplexRoutesOutboundByPlatformID(t *testing.T) {
	t.Parallel()

	cli := &stubPlatform{id: "cli"}
	slack := &stubPlatform{id: "slack"}
	m, err := multiplex.New(multiplex.Config{}, multiplex.Deps{
		Platforms: []agentkit.Platform{cli, slack},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := m.Send(context.Background(), agentkit.OutboundEvent{

		Type: agentkit.EventMessageEnd, Route: agentkit.RouteRef{Instance: "slack"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(cli.sent) != 0 {
		t.Fatalf("cli sent=%d, want 0", len(cli.sent))
	}
	if len(slack.sent) != 1 {
		t.Fatalf("slack sent=%d, want 1", len(slack.sent))
	}
}

func TestMultiplexReceiveSetsPlatformID(t *testing.T) {
	t.Parallel()

	cli := &stubPlatform{
		id: "cli",
		receive: []agentkit.MessageEvent{{
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
			},
		}},
	}
	slack := &stubPlatform{id: "slack"}

	m, err := multiplex.New(multiplex.Config{}, multiplex.Deps{
		Platforms: []agentkit.Platform{cli, slack},
	})
	if err != nil {
		t.Fatal(err)
	}

	event, err := m.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.Envelope.Route.Instance != "cli" {
		t.Fatalf("PlatformID=%q, want cli", event.Envelope.Route.Instance)
	}
	if text := event.Message.Content[0].Text; text != "hi" {
		t.Fatalf("text=%q, want hi", text)
	}

	_, err = m.Receive(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after all platforms closed, got %v", err)
	}
}

func TestMultiplexDisambiguatesDuplicatePlatformIDs(t *testing.T) {
	t.Parallel()

	first := &stubPlatform{id: "timer"}
	second := &stubPlatform{id: "timer"}
	m, err := multiplex.New(multiplex.Config{}, multiplex.Deps{
		Platforms: []agentkit.Platform{first, second},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := m.Send(context.Background(), agentkit.OutboundEvent{

		Type: agentkit.EventMessageEnd, Route: agentkit.RouteRef{Instance: "timer1"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(first.sent) != 0 {
		t.Fatalf("first sent=%d, want 0", len(first.sent))
	}
	if len(second.sent) != 1 {
		t.Fatalf("second sent=%d, want 1", len(second.sent))
	}
}

func TestMultiplexDuplicateProtocolPreservesInstanceRoute(t *testing.T) {
	t.Parallel()
	first := &stubPlatform{id: "slack"}
	second := &stubPlatform{id: "slack", receive: []agentkit.MessageEvent{{Envelope: agentkit.TurnEnvelope{Route: rctx.SessionRoute("slack", "slack:C1:t:1")}, Message: agentkit.ModelMessage{Role: "user"}}}}
	m, err := multiplex.New(multiplex.Config{}, multiplex.Deps{Platforms: []agentkit.Platform{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	event, err := m.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.Envelope.Route.Platform != "slack" || event.Envelope.Route.DestinationID() != "slack1" {
		t.Fatalf("instance route = %+v", event.Envelope.Route)
	}
	out := rctx.OutboundFromEnvelope(event.Envelope, agentkit.EventMessageEnd, nil)
	if err := m.Send(context.Background(), out); err != nil {
		t.Fatal(err)
	}
	if len(first.sent) != 0 || len(second.sent) != 1 {
		t.Fatalf("wrong adapter: first=%d second=%d", len(first.sent), len(second.sent))
	}
}

func TestExplicitInstanceDestinationUsesProtocolDelivery(t *testing.T) {
	a, b := &stubPlatform{id: "slack"}, &stubPlatform{id: "slack"}
	m, err := multiplex.New(multiplex.Config{}, multiplex.Deps{Platforms: []agentkit.Platform{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	route, err := rctx.ResolveRoute(context.Background(), delivery.RouteInput{SessionID: "slack1:C2"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Send(context.Background(), agentkit.OutboundEvent{Route: route, Type: agentkit.EventMessageEnd}); err != nil {
		t.Fatal(err)
	}
	got := b.sent[0].Route
	id, _ := rctx.RouteSessionID(got)
	if got.Platform != "slack" || id != "slack:C2" || got.Instance != "slack1" {
		t.Fatalf("route=%+v delivery=%s", got, id)
	}
}
