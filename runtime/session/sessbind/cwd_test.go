package sessbind_test

import (
	"context"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	"testing"
)

func TestCwdSessionChannelPrecedenceAndReset(t *testing.T) {
	store, ws := threadStore(t, "lark:oc_d95")
	ctx := larkThreadCtx("om_topicA")
	stack := sessbind.StackFromContext(ctx, ws).WithSession("session-a")
	kind := sessbind.CwdBinding{}
	for _, scope := range []sessbind.Level{sessbind.LevelChannel, sessbind.LevelSession} {
		value := "/project/" + scope.String()
		if err := stack.Set(ctx, store, kind, scope, value); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := stack.Effective(ctx, store, kind)
	if err != nil || got != "/project/session" {
		t.Fatalf("got %q %v", got, err)
	}
	if err := stack.Set(ctx, store, kind, sessbind.LevelSession, ""); err != nil {
		t.Fatal(err)
	}
	got, _, err = stack.Effective(ctx, store, kind)
	if err != nil || got != "/project/channel" {
		t.Fatalf("reset got %q %v", got, err)
	}
	other := sessbind.StackFromContext(larkThreadCtx("om_topicB"), ws).WithSession("session-b")
	got, _, err = other.Effective(context.Background(), store, kind)
	if err != nil || got != "/project/channel" {
		t.Fatalf("other topic got %q %v", got, err)
	}
}
