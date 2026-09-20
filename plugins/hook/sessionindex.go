package hook

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lengzhao/agentkit"
	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
	"github.com/lengzhao/pluginkit"
)

type SessionIndexConfig struct{}

type SessionIndexDeps struct {
	Index capsessionindex.Service `json:"index"`
}

type sessionIndexProvider struct {
	index capsessionindex.Service
}

func init() {
	pluginkit.Register("hook/session-index", NewSessionIndex)
}

// NewSessionIndex registers hook/session-index: refresh the session FTS index after each successful turn.
func NewSessionIndex(_ SessionIndexConfig, deps SessionIndexDeps) (agentkit.HookProvider, error) {
	if deps.Index == nil {
		return nil, fmt.Errorf("hook/session-index requires index")
	}
	return &sessionIndexProvider{
		index: deps.Index,
	}, nil
}

func (p *sessionIndexProvider) Hooks() []agentkit.Hook {
	return []agentkit.Hook{agentkit.OnTurnComplete(p.onTurnComplete)}
}

func (p *sessionIndexProvider) onTurnComplete(ctx context.Context, tc *agentkit.TurnComplete) error {
	if tc == nil {
		return nil
	}
	go func() {
		syncCtx := context.WithoutCancel(ctx)
		if err := p.index.SyncSessions(syncCtx); err != nil {
			slog.Warn("session index sync failed", "session_id", tc.SessionID, "err", err)
		}
	}()
	return nil
}
