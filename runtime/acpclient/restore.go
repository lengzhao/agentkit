package acpclient

import (
	"context"
	"errors"
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
	capacp "github.com/lengzhao/agentkit/cap/acp"
)

// RestoreStrategy selects how acp-remote re-attaches to a saved ACP session id.
type RestoreStrategy string

const (
	// StrategyResume only calls session/resume (legacy default when no plugin is injected).
	StrategyResume RestoreStrategy = "resume"
	// StrategyLoad only calls session/load (Cursor CLI documents load, not resume).
	StrategyLoad RestoreStrategy = "load"
	// StrategyAuto tries resume when advertised, then load when advertised.
	StrategyAuto RestoreStrategy = "auto"
	// StrategySkip never calls restore; always session/new (+ replay).
	StrategySkip RestoreStrategy = "skip"
	// StrategyCursor is an alias for load tuned for Cursor agent acp: Cursor does
	// not implement session/resume, but persists prompted sessions so that
	// session/load re-attaches after a subprocess restart (verified with the
	// session id returned by session/new; sessions never prompted are not yet
	// persisted, so load fails with "not found" and the caller falls back to
	// session/new, which is harmless because there is no history to lose).
	StrategyCursor RestoreStrategy = "cursor"
)

// Restorer implements capacp.SessionRestorer with a fixed strategy.
type Restorer struct {
	strategy RestoreStrategy
}

func NewRestorer(strategy RestoreStrategy) *Restorer {
	switch strategy {
	case "", StrategyResume:
		strategy = StrategyResume
	case StrategyCursor:
		strategy = StrategyLoad
	}
	return &Restorer{strategy: strategy}
}

func (r *Restorer) Restore(ctx context.Context, conn capacp.SessionRestoreConn, params capacp.SessionRestoreParams) (bool, error) {
	if r == nil || r.strategy == StrategySkip {
		return false, nil
	}
	methods := r.methods(params.AgentCapabilities)
	if len(methods) == 0 {
		slog.Debug("acp-remote: no restore method advertised by agent, creating new session", "session_id", params.HarnessSessionID, "strategy", r.strategy)
		return false, nil
	}
	var lastErr error
	for _, m := range methods {
		ok, err := tryRestoreMethod(ctx, conn, params, m)
		if err != nil {
			if restoreFailureLogLevel(err) == slog.LevelWarn {
				slog.Warn("acp-remote: session restore failed", "method", m, "session_id", params.HarnessSessionID, "acp_session_id", params.ACPSessionID, "err", err)
			} else {
				slog.Debug("acp-remote: session restore failed", "method", m, "session_id", params.HarnessSessionID, "acp_session_id", params.ACPSessionID, "err", err)
			}
			lastErr = err
			continue
		}
		if ok {
			slog.Info("acp-remote: restored acp session", "method", m, "session_id", params.HarnessSessionID, "acp_session_id", params.ACPSessionID)
			return true, nil
		}
	}
	if lastErr != nil {
		slog.Debug("acp-remote: restore unavailable, creating new session", "session_id", params.HarnessSessionID, "err", lastErr)
	}
	return false, nil
}

func (r *Restorer) methods(caps acp.AgentCapabilities) []string {
	switch r.strategy {
	case StrategyLoad:
		return []string{"session/load"}
	case StrategyAuto:
		var out []string
		if caps.SessionCapabilities.Resume != nil {
			out = append(out, "session/resume")
		}
		if caps.LoadSession {
			out = append(out, "session/load")
		}
		return out
	default:
		return []string{"session/resume"}
	}
}

func tryRestoreMethod(ctx context.Context, conn capacp.SessionRestoreConn, params capacp.SessionRestoreParams, method string) (bool, error) {
	switch method {
	case "session/resume":
		_, err := conn.ResumeSession(ctx, acp.ResumeSessionRequest{
			SessionId:  params.ACPSessionID,
			Cwd:        params.Cwd,
			McpServers: params.McpServers,
		})
		if err != nil {
			return false, err
		}
		return true, nil
	case "session/load":
		_, err := conn.LoadSession(ctx, acp.LoadSessionRequest{
			SessionId:  params.ACPSessionID,
			Cwd:        params.Cwd,
			McpServers: params.McpServers,
		})
		if err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, errors.New("unknown restore method: " + method)
	}
}

func restoreFailureLogLevel(err error) slog.Level {
	var re *acp.RequestError
	if errors.As(err, &re) {
		switch re.Code {
		case -32601, -32602:
			return slog.LevelDebug
		}
	}
	return slog.LevelWarn
}
