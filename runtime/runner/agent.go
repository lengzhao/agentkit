package runner

import (
	"context"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
)

func (r *Root) resolveAgentID(ctx context.Context, event agentkit.MessageEvent, conversation agentkit.SessionID) (agentkit.AgentID, error) {
	// entryKey keys the channel-scoped agent bind (/agent -c); keep it at channel
	// granularity so a bind set in one thread resolves from every thread.
	entryKey := rctx.ChannelEntryKeyFromContext(ctx)
	if entryKey == "" {
		entryKey = conversation
	}
	effective, _, _, _, err := sessbind.ResolveAgentID(ctx, r.sessionStore, r.workspace, conversation, entryKey, event.Envelope.AgentID)
	return effective, err
}

func (r *Root) resolveConversation(ctx context.Context, event agentkit.MessageEvent, env agentkit.TurnEnvelope, policy rctx.RoutePolicy) (string, error) {
	if capschedule.IsFireStateless(event.Envelope.Metadata) {
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
