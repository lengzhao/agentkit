package runner

import (
	"context"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/runtime/session"
)

func (r *Root) resolveAgentID(ctx context.Context, event agentkit.MessageEvent, conversation agentkit.SessionID) (agentkit.AgentID, error) {
	effective, _, _, err := session.ResolveAgentID(ctx, r.sessionStore, r.workspace, conversation, event.AgentID)
	return effective, err
}

func (r *Root) resolveConversation(ctx context.Context, event agentkit.MessageEvent, env agentkit.TurnEnvelope, policy session.RoutePolicy) (string, error) {
	if capschedule.IsFireStateless(event.Metadata) {
		return env.Conversation, nil
	}
	defaultConversation := env.Conversation
	activeStore, ok := r.sessionStore.(agentkit.ActiveSessionStore)
	if !ok {
		return defaultConversation, nil
	}
	entryKey := session.ActiveEntryKey(env.Route, policy, env.Actor.UserID)
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

