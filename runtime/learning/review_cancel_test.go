package learning

import (
	"context"
	"testing"
	"time"
)

func TestReviewRunRegistryCancelAll(t *testing.T) {
	reg := &ReviewRunRegistry{}
	ctx, _ := reg.Begin(context.Background(), "s1")
	if ctx.Err() != nil {
		t.Fatal("context should be active")
	}
	reg.CancelAll()
	if ctx.Err() == nil {
		t.Fatal("expected cancelled context after CancelAll")
	}
}

func TestCancelAllBackgroundReviews(t *testing.T) {
	orig := globalReviewRuns
	defer func() { globalReviewRuns = orig }()

	reg := &ReviewRunRegistry{}
	RegisterGlobalReviewRuns(reg)

	ctx, _ := reg.Begin(context.Background(), "shutdown-test")
	if ctx.Err() != nil {
		t.Fatal("context should be active")
	}
	CancelAllBackgroundReviews()
	waitDone(t, ctx)
}

func TestWaitBackgroundReviews(t *testing.T) {
	orig := globalReviewRuns
	defer func() { globalReviewRuns = orig }()

	reg := &ReviewRunRegistry{}
	RegisterGlobalReviewRuns(reg)

	done := make(chan struct{})
	Go(func() {
		defer close(done)
		time.Sleep(20 * time.Millisecond)
	})
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := WaitBackgroundReviews(waitCtx); err != nil {
		t.Fatalf("WaitBackgroundReviews: %v", err)
	}
	select {
	case <-done:
	default:
		t.Fatal("review goroutine did not finish")
	}
}

func waitDone(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled in time")
	}
}
