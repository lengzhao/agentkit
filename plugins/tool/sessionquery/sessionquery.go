package sessionquery

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session/sessindex"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	SessionsDir  string `json:"sessionsDir"`
	DefaultLimit int    `json:"defaultLimit"`
}

type Deps struct {
	Index     capsessionindex.Service `json:"index"`
	Workspace workspace.Service       `json:"workspace"`
}

// Input supports Hermes-style session_search shapes: search (FTS), list (browse sessions), scroll (context around a hit).
type Input struct {
	Mode string `json:"mode,omitempty" jsonschema:"search (default) | list | scroll"`
	// Search: FTS query. List: ignored. Scroll: optional when jumping to a session without a hit.
	Query string `json:"query,omitempty" jsonschema:"Keywords for mode=search"`
	// Scroll: session id from a prior search hit or list row.
	SessionID string `json:"session_id,omitempty" jsonschema:"Required for mode=scroll"`
	// Scroll: anchor event seq (from search hit); 0 means latest in session.
	Seq int64 `json:"seq,omitempty" jsonschema:"Anchor seq for mode=scroll"`
	// Scroll: lines before/after anchor (default 3 each when scroll and both zero).
	Before int `json:"before,omitempty"`
	After  int `json:"after,omitempty"`
	Limit  int `json:"limit,omitempty" jsonschema:"Max hits (search) or sessions (list); default 10, max 50"`
}

type Output struct {
	Mode      string                           `json:"mode"`
	Hits      []capsessionindex.Hit            `json:"hits,omitempty"`
	Sessions  []capsessionindex.SessionSummary `json:"sessions,omitempty"`
	Messages  []capsessionindex.MessageRow     `json:"messages,omitempty"`
	Count     int                              `json:"count"`
	SessionID string                           `json:"session_id,omitempty"`
	AnchorSeq int64                            `json:"anchor_seq,omitempty"`
}

func init() {
	pluginkit.Register("tool/session-query", New)
}

// New registers tool/session-search (alias kind tool/session-query): cross-session FTS, browse, and scroll.
func New(cfg Config, deps Deps) (agentkit.Tool, error) {
	if deps.Index == nil {
		return nil, fmt.Errorf("tool/session-search requires index")
	}
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/session-search requires workspace")
	}
	sessionsDir := strings.TrimSpace(cfg.SessionsDir)
	if sessionsDir == "" {
		sessionsDir = "sessions"
	}
	defaultLimit := cfg.DefaultLimit
	if defaultLimit <= 0 {
		defaultLimit = 10
	}
	idx := deps.Index
	ws := deps.Workspace
	tool, err := agentkit.NewTool[Input, Output]("session_search", func(ctx context.Context, input Input) (Output, error) {
		dir, err := ws.Resolve(ctx, sessionsDir)
		if err != nil {
			return Output{}, err
		}
		if err := sessindex.SyncSessionIndex(ctx, idx, dir); err != nil {
			return Output{}, err
		}
		mode := strings.ToLower(strings.TrimSpace(input.Mode))
		if mode == "" {
			mode = "search"
		}
		limit := input.Limit
		if limit <= 0 {
			limit = defaultLimit
		}
		if limit > 50 {
			limit = 50
		}
		switch mode {
		case "search":
			query := strings.TrimSpace(input.Query)
			if query == "" {
				return Output{}, fmt.Errorf("query is required for mode=search")
			}
			hits, err := idx.Search(ctx, query, limit)
			if err != nil {
				return Output{}, err
			}
			return Output{Mode: "search", Hits: hits, Count: len(hits)}, nil
		case "list", "browse":
			sessions, err := idx.ListSessions(ctx, limit)
			if err != nil {
				return Output{}, err
			}
			return Output{Mode: "list", Sessions: sessions, Count: len(sessions)}, nil
		case "scroll":
			sid := strings.TrimSpace(input.SessionID)
			if sid == "" {
				return Output{}, fmt.Errorf("session_id is required for mode=scroll")
			}
			msgs, err := idx.ScrollMessages(ctx, sid, input.Seq, input.Before, input.After)
			if err != nil {
				return Output{}, err
			}
			anchor := input.Seq
			if anchor <= 0 && len(msgs) > 0 {
				anchor = msgs[len(msgs)-1].Seq
			}
			return Output{
				Mode:      "scroll",
				SessionID: sid,
				AnchorSeq: anchor,
				Messages:  msgs,
				Count:     len(msgs),
			}, nil
		default:
			return Output{}, fmt.Errorf("mode must be search, list, or scroll")
		}
	}).
		Description("Search past conversations (mode=search), list recent sessions (mode=list), or scroll messages around a hit (mode=scroll with session_id and seq from search). Same-tenant only.").
		Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}
