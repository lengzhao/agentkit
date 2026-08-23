package compaction_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/lengzhao/agentkit/cap/compaction"
)

func TestRetryCallRecoversTransientError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	policy := compaction.RetryPolicy{Enabled: true, MaxRetries: 3, BaseDelayMs: 1}
	err := compaction.RetryCall(context.Background(), policy, func(err error) bool { return err != nil }, func() error {
		if calls.Add(1) == 1 {
			return errors.New("rate limit exceeded")
		}
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("retry call: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", calls.Load())
	}
}

func TestRetryCallDoesNotRetryQuotaError(t *testing.T) {
	t.Parallel()

	policy := compaction.RetryPolicy{Enabled: true, MaxRetries: 3, BaseDelayMs: 1}
	err := compaction.RetryCall(context.Background(), policy, func(err error) bool {
		return err != nil && err.Error() != "insufficient_quota"
	}, func() error {
		return errors.New("insufficient_quota")
	}, nil)
	if err == nil {
		t.Fatal("expected quota error")
	}
}

func TestRetryCallEmitsCallbacks(t *testing.T) {
	t.Parallel()

	var scheduled, finished int
	policy := compaction.RetryPolicy{Enabled: true, MaxRetries: 2, BaseDelayMs: 1}
	err := compaction.RetryCall(context.Background(), policy, func(err error) bool { return true }, func() error {
		return fmt.Errorf("temporary")
	}, &compaction.SummarizationRetryCallbacks{
		OnScheduled: func(attempt, maxAttempts, delayMs int, errorMessage string) {
			scheduled++
		},
		OnFinished: func(success bool, attempt int, finalError string) {
			finished++
			if success || attempt != 2 || finalError != "temporary" {
				t.Fatalf("unexpected finished callback: success=%v attempt=%d err=%q", success, attempt, finalError)
			}
		},
	})
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if scheduled != 2 || finished != 1 {
		t.Fatalf("scheduled=%d finished=%d", scheduled, finished)
	}
}
