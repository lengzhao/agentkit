package runner

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rtschedule "github.com/lengzhao/agentkit/runtime/schedule"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
)

func (r *Root) resolveAgentID(ctx context.Context, event agentkit.MessageEvent, conversation agentkit.SessionID) (agentkit.AgentID, error) {
	effective, _, _, err := sessbind.ResolveAgentID(ctx, r.sessionStore, r.workspace, conversation, event.AgentID)
	return effective, err
}

func (r *Root) resolveConversation(ctx context.Context, event agentkit.MessageEvent, env agentkit.TurnEnvelope, policy rctx.RoutePolicy) (string, error) {
	if rtschedule.IsFireStateless(event.Metadata) {
		return env.Conversation, nil
	}
	defaultConversation := env.Conversation
	activeStore, ok := r.sessionStore.(agentkit.ActiveSessionStore)
	if !ok {
		return defaultConversation, nil
	}
	entryKey := rctx.ActiveEntryKey(env.Route, policy, env.Actor.UserID)
	if entryKey == "" {
		return defaultConversation, nil
	}
	active, err := activeStore.ActiveSession(ctx, entryKey)
	if err != nil {
		return "", err
	}
	if active != entryKey {
		return string(active), nil
	}
	return defaultConversation, nil
}
