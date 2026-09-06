package agent

import (
	"context"
	"errors"
	"testing"
)

func TestCancelReasonFromError(t *testing.T) {
	reason, ok := cancelReasonFromError(errors.New("cancelled: /stop"))
	if !ok || reason != "/stop" {
		t.Fatalf("reason = %q, ok = %v", reason, ok)
	}
	reason, ok = cancelReasonFromError(context.Canceled)
	if !ok || reason != "cancelled" {
		t.Fatalf("reason = %q, ok = %v", reason, ok)
	}
	if _, ok := cancelReasonFromError(errors.New("boom")); ok {
		t.Fatal("expected non-cancel error to be ignored")
	}
}
