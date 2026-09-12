package prompt

import (
	"context"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

type frozenSnapshot struct {
	Content string
}

var memoryTurnCache sync.Map // turnCacheKey -> *frozenSnapshot

type turnCacheKey struct {
	session string
	turn    string
}

func turnCacheKeyFrom(ctx context.Context) (turnCacheKey, bool) {
	sessionID := session.ConversationFromContext(ctx)
	turnID, _ := ctx.Value(agentkit.KeyTurnID).(string)
	if sessionID == "" || turnID == "" {
		return turnCacheKey{}, false
	}
	return turnCacheKey{session: sessionID, turn: turnID}, true
}

func loadFrozenMemory(ctx context.Context, load func() (string, error)) (string, error) {
	key, ok := turnCacheKeyFrom(ctx)
	if !ok {
		return load()
	}
	if v, ok := memoryTurnCache.Load(key); ok {
		if snap, ok := v.(*frozenSnapshot); ok {
			return snap.Content, nil
		}
	}
	content, err := load()
	if err != nil {
		return "", err
	}
	memoryTurnCache.Store(key, &frozenSnapshot{Content: content})
	return content, nil
}

