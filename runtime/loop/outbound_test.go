package loop

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

func TestAsyncEmitterReturnsImmediately(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	var calls atomic.Int32
	inner := agentkit.OutboundEmit(func(context.Context, agentkit.OutboundEvent) error {
		calls.Add(1)
		<-block
		return nil
	})

	emitter := NewAsyncEmitter(inner)
	if emitter == nil {
		t.Fatal("expected emitter")
	}
	defer emitter.Close()

	start := time.Now()
	if err := emitter.Emit(context.Background(), agentkit.OutboundEvent{Type: agentkit.EventTurnStart}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("non-blocking emit took %v", elapsed)
	}

	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	close(block)
}

func TestAsyncEmitterPreservesOrder(t *testing.T) {
	t.Parallel()

	var got []int
	var done atomic.Int32
	inner := agentkit.OutboundEmit(func(_ context.Context, event agentkit.OutboundEvent) error {
		var n int
		// payload carries the index in Data as a single byte for test simplicity
		if len(event.Data) > 0 {
			n = int(event.Data[0])
		}
		got = append(got, n)
		if n == 9 {
			done.Store(1)
		}
		return nil
	})

	emitter := NewAsyncEmitter(inner)
	if emitter == nil {
		t.Fatal("expected emitter")
	}
	for i := 0; i < 10; i++ {
		_ = emitter.Emit(context.Background(), agentkit.OutboundEvent{
			Type: agentkit.EventMessageUpdate,
			Data: []byte{byte(i)},
		})
	}
	emitter.Close()

	deadline := time.Now().Add(2 * time.Second)
	for done.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if done.Load() == 0 {
		t.Fatal("last event not delivered before close")
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("got[%d] = %d, want %d (order not preserved)", i, v, i)
		}
	}
}

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
	// forwardParentEmit which holds the AsyncEmitter.Close path.
	if err := emit(ctx, agentkit.OutboundEvent{Type: agentkit.EventTurnEnd}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}
