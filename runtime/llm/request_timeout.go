package llm

import (
	"context"
	"time"

	"github.com/lengzhao/agentkit"
)

const defaultRequestTimeout = 180 * time.Second

func resolveRequestTimeout(timeoutSeconds int) time.Duration {
	if timeoutSeconds > 0 {
		return time.Duration(timeoutSeconds) * time.Second
	}
	return defaultRequestTimeout
}

func mergeRequestTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.WithTimeout(ctx, 0)
		}
		if remaining <= timeout {
			return ctx, func() {}
		}
	}
	return context.WithTimeout(ctx, timeout)
}

// streamOutputStarted is true when the model has begun producing output (text,
// thinking, tool call, or a one-shot message). Used to end the TTFB timer.
func streamOutputStarted(ev agentkit.LLMEvent, err error) bool {
	if err != nil {
		return true
	}
	switch ev.Type {
	case agentkit.AssistantEventTextDelta,
		agentkit.AssistantEventThinkingDelta,
		agentkit.AssistantEventToolCallStart,
		agentkit.AssistantEventToolCallDelta,
		agentkit.LLMEventMessage:
		return true
	default:
		return false
	}
}

type contextBoundStream struct {
	parent context.Context
	ttfb   context.Context
	cancel context.CancelFunc
	inner  agentkit.LLMStream
}

func streamWithRequestTimeout(parent context.Context, ttfb context.Context, cancel context.CancelFunc, inner agentkit.LLMStream) agentkit.LLMStream {
	if inner == nil {
		cancel()
		return &contextBoundStream{parent: parent, cancel: cancel, inner: nil}
	}
	return &contextBoundStream{
		parent: parent,
		ttfb:   ttfb,
		cancel: cancel,
		inner:  inner,
	}
}

func (s *contextBoundStream) Recv() (agentkit.LLMEvent, error) {
	if err := s.parent.Err(); err != nil {
		return agentkit.LLMEvent{}, err
	}
	if s.inner == nil {
		return agentkit.LLMEvent{}, s.parent.Err()
	}
	if s.ttfb == nil {
		return s.inner.Recv()
	}
	type result struct {
		ev  agentkit.LLMEvent
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ev, err := s.inner.Recv()
		ch <- result{ev: ev, err: err}
	}()
	select {
	case <-s.ttfb.Done():
		_ = s.inner.Close()
		return agentkit.LLMEvent{}, s.ttfb.Err()
	case r := <-ch:
		if streamOutputStarted(r.ev, r.err) {
			s.cancel()
			s.ttfb = nil
		}
		return r.ev, r.err
	}
}

func (s *contextBoundStream) Close() error {
	s.cancel()
	if s.inner == nil {
		return nil
	}
	return s.inner.Close()
}
