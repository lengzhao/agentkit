package multiplex_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/multiplex"
)

type stubPlatform struct {
	receive []agentkit.MessageEvent
	recvErr []error
	sent    []agentkit.OutboundEvent
}

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

func TestMultiplexRoutesOutboundByPlatformID(t *testing.T) {
	t.Parallel()

	cli := &stubPlatform{}
	slack := &stubPlatform{}
	m, err := multiplex.New(multiplex.Config{Names: []string{"cli", "slack"}}, multiplex.Deps{
		Platforms: []agentkit.Platform{cli, slack},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := m.Send(context.Background(), agentkit.OutboundEvent{
		PlatformID: "slack",
		Type:       agentkit.EventMessageEnd,
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
		receive: []agentkit.MessageEvent{{
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
			},
		}},
	}
	slack := &stubPlatform{}

	m, err := multiplex.New(multiplex.Config{Names: []string{"cli", "slack"}}, multiplex.Deps{
		Platforms: []agentkit.Platform{cli, slack},
	})
	if err != nil {
		t.Fatal(err)
	}

	event, err := m.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.PlatformID != "cli" {
		t.Fatalf("PlatformID=%q, want cli", event.PlatformID)
	}
	if text := event.Message.Content[0].Text; text != "hi" {
		t.Fatalf("text=%q, want hi", text)
	}

	_, err = m.Receive(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after all platforms closed, got %v", err)
	}
}
