package runner

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/runtime/session"
)

func (r *Root) resolveAgentID(ctx context.Context, event agentkit.MessageEvent, sessionID agentkit.SessionID) (agentkit.AgentID, error) {
	if id := strings.TrimSpace(string(event.AgentID)); id != "" {
		return agentkit.AgentID(id), nil
	}
	if bound, err := r.boundAgent(ctx, sessionID); err != nil {
		return "", err
	} else if bound != "" {
		return bound, nil
	}
	return "", nil
}

func (r *Root) resolveSessionID(ctx context.Context, event agentkit.MessageEvent, effectiveSessionID agentkit.SessionID) (agentkit.SessionID, error) {
	if capschedule.IsFireStateless(event.Metadata) {
		return effectiveSessionID, nil
	}
	deliveryID := r.inboundDeliveryID(event)
	if deliveryID == "" {
		deliveryID = event.SessionID
	}
	defaultID := effectiveSessionID
	if event.PlatformID == "chat-api" && deliveryID != "" {
		defaultID = deliveryID
	}
	activeStore, ok := r.sessionStore.(agentkit.ActiveSessionStore)
	if !ok {
		return defaultID, nil
	}
	entryKey := session.ActiveSessionEntryKey(event.PlatformID, deliveryID, r.sessionScope, event.UserID)
	if entryKey != "" {
		active, err := activeStore.ActiveSession(ctx, entryKey)
		if err != nil {
			return "", err
		}
		if active != entryKey {
			return active, nil
		}
	}
	return defaultID, nil
}

func (r *Root) boundAgent(ctx context.Context, sessionID agentkit.SessionID) (agentkit.AgentID, error) {
	if r.sessionStore == nil || sessionID == "" {
		return "", nil
	}
	bindStore, ok := r.sessionStore.(agentkit.AgentBindStore)
	if !ok {
		return "", nil
	}
	return bindStore.AgentBind(ctx, sessionID)
}
