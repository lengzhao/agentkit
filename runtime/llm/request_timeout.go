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

type contextBoundStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	inner  agentkit.LLMStream
}

func streamWithRequestTimeout(ctx context.Context, timeout time.Duration, inner agentkit.LLMStream) agentkit.LLMStream {
	ctx, cancel := mergeRequestTimeout(ctx, timeout)
	if inner == nil {
		cancel()
		return &contextBoundStream{ctx: ctx, cancel: cancel, inner: nil}
	}
	return &contextBoundStream{ctx: ctx, cancel: cancel, inner: inner}
}

func (s *contextBoundStream) Recv() (agentkit.LLMEvent, error) {
	if err := s.ctx.Err(); err != nil {
		return agentkit.LLMEvent{}, err
	}
	if s.inner == nil {
		return agentkit.LLMEvent{}, s.ctx.Err()
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
	case <-s.ctx.Done():
		_ = s.inner.Close()
		return agentkit.LLMEvent{}, s.ctx.Err()
	case r := <-ch:
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
