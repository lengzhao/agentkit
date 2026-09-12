package hook

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/pluginkit"
)

type SessionIndexConfig struct {
	SessionsDir string `json:"sessionsDir"`
}

type SessionIndexDeps struct {
	Index     capsessionindex.Service `json:"index"`
	Workspace workspace.Service       `json:"workspace"`
}

type sessionIndexProvider struct {
	sessionsDir string
	index       capsessionindex.Service
	workspace   workspace.Service
}

func init() {
	pluginkit.Register("hook/session-index", NewSessionIndex)
}

// NewSessionIndex registers hook/session-index: refresh the session FTS index after each successful turn.
func NewSessionIndex(cfg SessionIndexConfig, deps SessionIndexDeps) (agentkit.HookProvider, error) {
	if deps.Index == nil {
		return nil, fmt.Errorf("hook/session-index requires index")
	}
	if deps.Workspace == nil {
		return nil, fmt.Errorf("hook/session-index requires workspace")
	}
	dir := strings.TrimSpace(cfg.SessionsDir)
	if dir == "" {
		dir = "sessions"
	}
	return &sessionIndexProvider{
		sessionsDir: dir,
		index:       deps.Index,
		workspace:   deps.Workspace,
	}, nil
}

func (p *sessionIndexProvider) Hooks() []agentkit.Hook {
	return []agentkit.Hook{agentkit.OnTurnComplete(p.onTurnComplete)}
}

func (p *sessionIndexProvider) onTurnComplete(ctx context.Context, tc *agentkit.TurnComplete) error {
	if tc == nil {
		return nil
	}
	dir, err := p.workspace.Resolve(ctx, p.sessionsDir)
	if err != nil {
		slog.Debug("session index sync skipped", "err", err)
		return nil
	}
	go func() {
		syncCtx := context.WithoutCancel(ctx)
		if err := p.index.SyncSessions(syncCtx, dir); err != nil {
			slog.Warn("session index sync failed", "session_id", tc.SessionID, "err", err)
		}
	}()
	return nil
}
