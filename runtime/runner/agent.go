package runner

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
)

func (r *Root) resolveAgentID(ctx context.Context, event agentkit.MessageEvent, storeSessionID agentkit.SessionID) (agentkit.AgentID, error) {
	if id := strings.TrimSpace(string(event.AgentID)); id != "" {
		return agentkit.AgentID(id), nil
	}
	if event.SessionID != "" && event.SessionID != storeSessionID {
		if bound, err := r.boundAgent(ctx, event.SessionID); err != nil {
			return "", err
		} else if bound != "" {
			return bound, nil
		}
	}
	if bound, err := r.boundAgent(ctx, storeSessionID); err != nil {
		return "", err
	} else if bound != "" {
		return bound, nil
	}
	return "", nil
}

func (r *Root) resolveStoreSessionID(ctx context.Context, event agentkit.MessageEvent, effectiveSessionID agentkit.SessionID) (agentkit.SessionID, error) {
	deliveryID := event.SessionID
	defaultID := effectiveSessionID
	if event.PlatformID == "chat-api" && deliveryID != "" {
		defaultID = deliveryID
	}
	activeStore, ok := r.sessionStore.(agentkit.ActiveSessionStore)
	if !ok {
		return defaultID, nil
	}
	if deliveryID != "" {
		active, err := activeStore.ActiveSession(ctx, deliveryID)
		if err != nil {
			return "", err
		}
		if active != deliveryID {
			return active, nil
		}
	}
	if effectiveSessionID != "" && effectiveSessionID != deliveryID {
		active, err := activeStore.ActiveSession(ctx, effectiveSessionID)
		if err != nil {
			return "", err
		}
		if active != effectiveSessionID {
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
