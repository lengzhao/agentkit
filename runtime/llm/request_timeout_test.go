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

func TestStreamWithRequestTimeoutRespectsParentCancelDuringTTFB(t *testing.T) {
	parent, stop := context.WithCancel(context.Background())
	ttfbCtx, cancel := mergeRequestTimeout(parent, time.Minute)
	inner := &blockingRecvStream{delay: 5 * time.Second}
	wrapped := streamWithRequestTimeout(parent, ttfbCtx, cancel, inner)
	stop()
	ev, err := wrapped.Recv()
	if err == nil {
		t.Fatalf("expected cancel, got event %+v", ev)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

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

// ctxBoundStream simulates OpenAI HTTP streams tied to the context passed at create time.
type ctxBoundStream struct {
	lifetime context.Context
	calls    int
}

func (s *ctxBoundStream) Recv() (agentkit.LLMEvent, error) {
	s.calls++
	if s.calls == 1 {
		return agentkit.LLMEvent{Type: agentkit.AssistantEventTextDelta, Delta: "hi"}, nil
	}
	if err := s.lifetime.Err(); err != nil {
		return agentkit.LLMEvent{}, err
	}
	return agentkit.LLMEvent{}, io.EOF
}

func (s *ctxBoundStream) Close() error { return nil }

func TestStreamWithRequestTimeoutAllowsTailWhenInnerUsesParentContext(t *testing.T) {
	parent := context.Background()
	ttfbCtx, ttfbCancel := mergeRequestTimeout(parent, time.Minute)
	inner := &ctxBoundStream{lifetime: parent}
	wrapped := streamWithRequestTimeout(parent, ttfbCtx, ttfbCancel, inner)

	ev, err := wrapped.Recv()
	if err != nil || ev.Delta != "hi" {
		t.Fatalf("first recv: ev=%+v err=%v", ev, err)
	}
	_, err = wrapped.Recv()
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("tail recv: %v", err)
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

func TestResolveResponseHeaderTimeoutDefault(t *testing.T) {
	if resolveResponseHeaderTimeout(0) != defaultResponseHeaderTimeout {
		t.Fatalf("expected default %v", defaultResponseHeaderTimeout)
	}
	if resolveResponseHeaderTimeout(30) != 30*time.Second {
		t.Fatal("expected 30s")
	}
}
