package rctx

import (
	"context"
	"log/slog"
	"time"

	"github.com/lengzhao/agentkit"
)

// asyncEmitTimeout bounds each outbound delivery call made by AsyncEmitter's
// loop goroutine. Platform I/O (e.g. Feishu card patch) must not stall the
// emitter forever: a single hung HTTP request would otherwise block Close,
// which is on the async-subagent completion path. The deadline is per-event;
// normal fast calls are unaffected.
const asyncEmitTimeout = 15 * time.Second

// AsyncEmitter wraps an OutboundEmit so calls return immediately while events
// are delivered in arrival order by a single goroutine. This keeps async
// subagent outbound (e.g. ACP SessionUpdate) from blocking the child agent
// runtime on platform I/O, while preserving event order for progress cards.
// Close must be called when the emitter is no longer needed to retire the
// goroutine; events still queued are best-effort drained.
type AsyncEmitter struct {
	emit agentkit.OutboundEmit
	ch   chan agentkit.OutboundEvent
	done chan struct{}
}

// NewAsyncEmitter returns an emitter that serializes events through a single
// goroutine. Returns nil when inner is nil so callers can assign directly.
func NewAsyncEmitter(inner agentkit.OutboundEmit) *AsyncEmitter {
	if inner == nil {
		return nil
	}
	e := &AsyncEmitter{
		emit: inner,
		ch:   make(chan agentkit.OutboundEvent, 256),
		done: make(chan struct{}),
	}
	go e.loop()
	return e
}

func (e *AsyncEmitter) loop() {
	defer close(e.done)
	for ev := range e.ch {
		ctx, cancel := context.WithTimeout(context.Background(), asyncEmitTimeout)
		if err := e.emit(ctx, ev); err != nil {
			slog.Debug("outbound async delivery failed", "event", ev.Type, "err", err)
		}
		cancel()
	}
}

// Emit is the OutboundEmit entry point. It never blocks on platform I/O; when
// the queue is full the event is dropped with a warning rather than stalling
// the caller or growing without bound.
func (e *AsyncEmitter) Emit(_ context.Context, event agentkit.OutboundEvent) error {
	select {
	case e.ch <- event:
		return nil
	case <-e.done:
		return nil
	default:
		slog.Warn("outbound async queue full, dropping event", "event", event.Type)
		return nil
	}
}

// Close drains the queue and retires the goroutine. Safe to call once.
func (e *AsyncEmitter) Close() {
	close(e.ch)
	<-e.done
}

// CloseWithTimeout drains the queue like Close but gives up waiting after d.
// It returns true if the loop retired within d, false if it is still draining
// (the loop goroutine keeps running best-effort and retires once the queue is
// empty). Use this on paths where blocking forever on platform I/O would stall
// a more important step (e.g. async subagent follow-up delivery): the progress
// card is best-effort UI and must not block the follow-up turn.
func (e *AsyncEmitter) CloseWithTimeout(d time.Duration) bool {
	close(e.ch)
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-e.done:
		return true
	case <-timer.C:
		slog.Warn("outbound async emitter close timed out, loop still draining", "timeout", d)
		return false
	}
}
