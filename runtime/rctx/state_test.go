package rctx_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestStateSetGetDelete(t *testing.T) {
	t.Parallel()
	st := rctx.NewState()
	if _, ok := st.Get("k"); ok {
		t.Fatal("empty state must miss")
	}
	st.Set("k", "v")
	st.Set("k", "v2")
	if v, ok := st.Get("k"); !ok || v != "v2" {
		t.Fatalf("got %v %v", v, ok)
	}
	st.Delete("k")
	if _, ok := st.Get("k"); ok {
		t.Fatal("deleted key must miss")
	}
}

func TestStateFromContext(t *testing.T) {
	t.Parallel()
	if rctx.StateFrom(context.Background()) != nil {
		t.Fatal("no injection must return nil")
	}
	st := rctx.NewState()
	ctx := rctx.WithState(context.Background(), st)
	if rctx.StateFrom(ctx) != st {
		t.Fatal("must return the injected bag")
	}
	// Downstream writes are visible through the same pointer.
	rctx.StateFrom(ctx).Set("tools/deferred.revealed", []string{"x"})
	if v, _ := st.Get("tools/deferred.revealed"); v.([]string)[0] != "x" {
		t.Fatalf("shared bag not visible: %v", v)
	}
}

func TestWithStateNil(t *testing.T) {
	t.Parallel()
	ctx := rctx.WithState(context.Background(), nil)
	if rctx.StateFrom(ctx) != nil {
		t.Fatal("nil bag must stay unset")
	}
}
