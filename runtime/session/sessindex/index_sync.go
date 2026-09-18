package sessindex

import (
	"context"
	"strings"

	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
)

// SyncSessionIndex refreshes the SQLite FTS index from a sessions directory.
func SyncSessionIndex(ctx context.Context, index capsessionindex.Service, sessionsDir string) error {
	if index == nil {
		return nil
	}
	return index.SyncSessions(ctx, sessionsDir)
}

// SearchSyncedSessions syncs then runs FTS over indexed messages.
func SearchSyncedSessions(ctx context.Context, index capsessionindex.Service, sessionsDir, query string, limit int) ([]capsessionindex.Hit, error) {
	if index == nil {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if err := SyncSessionIndex(ctx, index, sessionsDir); err != nil {
		return nil, err
	}
	return index.Search(ctx, query, limit)
}
