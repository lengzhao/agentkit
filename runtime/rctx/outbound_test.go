package rctx

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestOutboundEmitFromContextReturnsRawEmit(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	ctx := ContextWithOutboundEmit(context.Background(), func(context.Context, agentkit.OutboundEvent) error {
		calls.Add(1)
		return nil
	})
	ctx = context.WithValue(ctx, agentkit.KeyAsyncSubagent, true)

	emit := OutboundEmitFromContext(ctx)
	if emit == nil {
		t.Fatal("expected emit")
	}
	// OutboundEmitFromContext returns the raw hook; async wrapping is owned by
	// subagent.forwardParentEmit which holds the AsyncEmitter.Close path.
	if err := emit(ctx, agentkit.OutboundEvent{Type: agentkit.EventTurnEnd}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestMarshalOutboundData(t *testing.T) {
	t.Parallel()

	raw := MarshalOutboundData(map[string]string{"k": "v"})
	if string(raw) != `{"k":"v"}` {
		t.Fatalf("got %s", raw)
	}
	if got := MarshalOutboundData(func() {}); got != nil {
		t.Fatalf("unmarshalable payload should yield nil, got %s", got)
	}
}
