package rctx_test

import (
	"context"
	"reflect"
	"testing"

	capexecution "github.com/lengzhao/agentkit/cap/execution"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestExecutionIdentityIsSeparateFromSessionContext(t *testing.T) {
	identity := capexecution.Identity{ID: "run-2", ParentID: "run-1", RootTurnID: "turn-1"}
	snapshot := rctx.SessionContext{SessionID: "history", Cwd: "/project", Epoch: "epoch-1"}
	ctx := rctx.WithExecutionIdentity(context.Background(), identity)
	ctx = rctx.WithSessionContext(ctx, snapshot)
	if got := rctx.ExecutionIdentityFromContext(ctx); got != identity {
		t.Fatalf("identity=%+v", got)
	}
	if got, ok := rctx.ExecutionFromContext(ctx); !ok || got != snapshot {
		t.Fatalf("legacy snapshot=%+v ok=%v", got, ok)
	}
	ctx = rctx.WithoutSessionContext(ctx)
	if got := rctx.ExecutionIdentityFromContext(ctx); got != identity {
		t.Fatal("clearing history context cleared run identity")
	}
	if got, _ := rctx.SessionContextFromContext(ctx); got != (rctx.SessionContext{}) {
		t.Fatalf("snapshot not cleared: %+v", got)
	}
	ctx = rctx.WithExecution(ctx, rctx.Execution{SessionID: "old-api"})
	if got, _ := rctx.SessionContextFromContext(ctx); got.SessionID != "old-api" {
		t.Fatal("legacy writer diverged from new reader")
	}
}

func TestLegacyExecutionRetainsRuntimeTypeIdentity(t *testing.T) {
	legacy := reflect.TypeOf(rctx.Execution{})
	if legacy.Name() != "Execution" {
		t.Fatalf("legacy runtime type changed to %s", legacy.Name())
	}
	if reflect.TypeOf(rctx.SessionContext{}) != legacy {
		t.Fatal("new name is not an alias of legacy type")
	}
}
