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

func TestStreamWithRequestTimeoutCancelsSlowRecv(t *testing.T) {
	ctx := context.Background()
	stream := streamWithRequestTimeout(ctx, 50*time.Millisecond, &blockingRecvStream{delay: 2 * time.Second})
	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
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
