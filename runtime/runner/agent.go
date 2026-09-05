package runner

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/runtime/session"
)

func (r *Root) resolveAgentID(ctx context.Context, event agentkit.MessageEvent, conversation agentkit.SessionID) (agentkit.AgentID, error) {
	if id := strings.TrimSpace(string(event.AgentID)); id != "" {
		return agentkit.AgentID(id), nil
	}
	if bound, err := r.boundAgent(ctx, conversation); err != nil {
		return "", err
	} else if bound != "" {
		return bound, nil
	}
	return "", nil
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

func (r *Root) boundAgent(ctx context.Context, conversation agentkit.SessionID) (agentkit.AgentID, error) {
	if r.sessionStore == nil || conversation == "" {
		return "", nil
	}
	bindStore, ok := r.sessionStore.(agentkit.AgentBindStore)
	if !ok {
		return "", nil
	}
	return bindStore.AgentBind(ctx, conversation)
}
