package llm

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

type blockingRecvStream struct {
	delay time.Duration
}

func (b *blockingRecvStream) Recv() (agentkit.LLMEvent, error) {
	time.Sleep(b.delay)
	return agentkit.LLMEvent{}, io.EOF
}

func (b *blockingRecvStream) Close() error { return nil }

type slowTailStream struct {
	calls int
	delay time.Duration
}

func (s *slowTailStream) Recv() (agentkit.LLMEvent, error) {
	s.calls++
	if s.calls == 1 {
		return agentkit.LLMEvent{Type: agentkit.AssistantEventTextDelta, Delta: "hi"}, nil
	}
	time.Sleep(s.delay)
	return agentkit.LLMEvent{}, io.EOF
}

func (s *slowTailStream) Close() error { return nil }

func TestStreamWithRequestTimeoutCancelsSlowFirstRecv(t *testing.T) {
	ctx := context.Background()
	ttfbCtx, cancel := mergeRequestTimeout(ctx, 50*time.Millisecond)
	stream := streamWithRequestTimeout(ctx, ttfbCtx, cancel, &blockingRecvStream{delay: 2 * time.Second})
	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestStreamWithRequestTimeoutAllowsSlowTailAfterFirstToken(t *testing.T) {
	ctx := context.Background()
	ttfbCtx, cancel := mergeRequestTimeout(ctx, 50*time.Millisecond)
	stream := streamWithRequestTimeout(ctx, ttfbCtx, cancel, &slowTailStream{delay: 200 * time.Millisecond})
	ev, err := stream.Recv()
	if err != nil {
		t.Fatalf("first recv: %v", err)
	}
	if ev.Delta != "hi" {
		t.Fatalf("delta = %q", ev.Delta)
	}
	_, err = stream.Recv()
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("tail recv: %v", err)
	}
}

func TestResolveRequestTimeoutDefault(t *testing.T) {
	if resolveRequestTimeout(0) != defaultRequestTimeout {
		t.Fatalf("expected default %v", defaultRequestTimeout)
	}
	if resolveRequestTimeout(120) != 120*time.Second {
		t.Fatal("expected 120s")
	}
}
