package sessionquery

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
	"github.com/lengzhao/agentkit/cap/workspace"
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

type Input struct {
	Query string `json:"query" jsonschema:"Keywords or phrase to search across past sessions in this tenant"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum hits (default 10, max 50)"`
}

type Output struct {
	Hits  []capsessionindex.Hit `json:"hits"`
	Count int                   `json:"count"`
}

func init() {
	pluginkit.Register("tool/session-query", New)
}

// New registers tool/session-query: full-text search over durable session logs for the current tenant.
func New(cfg Config, deps Deps) (agentkit.Tool, error) {
	if deps.Index == nil {
		return nil, fmt.Errorf("tool/session-query requires index")
	}
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/session-query requires workspace")
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
	return agentkit.NewTool[Input, Output]("session_query", func(ctx context.Context, input Input) (Output, error) {
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return Output{}, fmt.Errorf("query is required")
		}
		dir, err := ws.Resolve(ctx, sessionsDir)
		if err != nil {
			return Output{}, err
		}
		if err := idx.SyncSessions(ctx, dir); err != nil {
			return Output{}, err
		}
		limit := input.Limit
		if limit <= 0 {
			limit = defaultLimit
		}
		hits, err := idx.Search(ctx, query, limit)
		if err != nil {
			return Output{}, err
		}
		return Output{Hits: hits, Count: len(hits)}, nil
	}).
		Description("Search past conversation transcripts in this workspace (all sessions for the current tenant). Use when the user asks whether something was discussed before or you need cross-thread context.").
		Build()
}
