package sessbind_test

import (
	"context"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	"testing"
)

func TestNewSessionDoesNotInheritInitialSessionOverrides(t *testing.T) {
	store, ws := threadStore(t, "lark:oc_d95")
	ctx := larkThreadCtx("om_topicA")
	original := sessbind.StackFromContext(ctx, ws).Keys.Thread
	if err := sessbind.SetSessionAgentBind(context.Background(), store, original, "old-agent"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetSessionModelBind(context.Background(), store, original, "old-model"); err != nil {
		t.Fatal(err)
	}
	agent, _, _, _, err := sessbind.ResolveAgentID(ctx, store, ws, "lark:oc_d95:new-child", "", "default-agent")
	if err != nil {
		t.Fatal(err)
	}
	model, _, _, err := sessbind.ResolveModelBinding(ctx, store, ws, "lark:oc_d95:new-child", "default-agent", "default-model")
	if err != nil {
		t.Fatal(err)
	}
	if agent != "default-agent" || model != "default-model" {
		t.Fatalf("new session inherited old session overrides: agent=%s model=%s", agent, model)
	}
}

func TestScopedBindingsDoNotOverwriteSessionOverrides(t *testing.T) {
	for _, level := range []sessbind.Level{sessbind.LevelThread, sessbind.LevelUser} {
		t.Run(level.String(), func(t *testing.T) {
			store, ws := threadStore(t, "lark:oc_d95")
			ctx := larkThreadCtx("om_topicA")
			stack := sessbind.StackFromContext(ctx, ws)
			key := stack.Keys.Thread
			if level == sessbind.LevelUser {
				key = stack.Keys.User
			}
			for _, kind := range []sessbind.Binding{sessbind.AgentBinding{}, sessbind.ModelBinding{}} {
				t.Run(kind.Key(), func(t *testing.T) {
					session := stack.WithSession(key)
					if err := session.Set(ctx, store, kind, sessbind.LevelSession, "session-value"); err != nil {
						t.Fatal(err)
					}
					if err := stack.Set(ctx, store, kind, level, "scope-value"); err != nil {
						t.Fatal(err)
					}
					value, winner, _, err := session.EffectiveWithLevel(ctx, store, kind)
					if err != nil || value != "session-value" || winner != sessbind.LevelSession {
						t.Fatalf("session changed: value=%s winner=%v err=%v", value, winner, err)
					}
					child := stack.WithSession("lark:oc_d95:new-child")
					value, winner, _, err = child.EffectiveWithLevel(ctx, store, kind)
					if err != nil || value != "scope-value" || winner != level {
						t.Fatalf("scope missing: value=%s winner=%v err=%v", value, winner, err)
					}
					if err := stack.Set(ctx, store, kind, level, ""); err != nil {
						t.Fatal(err)
					}
					value, _, _, err = session.EffectiveWithLevel(ctx, store, kind)
					if err != nil || value != "session-value" {
						t.Fatalf("scope reset changed session: %s %v", value, err)
					}
				})
			}
		})
	}
}
