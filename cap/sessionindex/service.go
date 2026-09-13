package sessionindex

import (
	"context"
	"time"
)

// Hit is one FTS match in a session transcript.
type Hit struct {
	SessionID string
	Seq       int64
	Role      string
	Snippet   string
}

// SessionSummary is a indexed session row for discovery / browse.
type SessionSummary struct {
	SessionID    string
	MessageCount int
	LastSeq      int64
	// LastMod is the session JSONL mtime at last index sync (append activity).
	LastMod time.Time
}

// MessageRow is one indexed transcript line for scroll / browse.
type MessageRow struct {
	Seq  int64
	Role string
	Text string
}

// Service indexes durable session JSONL logs and supports full-text search per tenant workspace.
type Service interface {
	// SyncSessions ingests or refreshes index entries from a sessions directory.
	SyncSessions(ctx context.Context, sessionsDir string) error
	// Search runs FTS over indexed messages for the current workspace database.
	Search(ctx context.Context, query string, limit int) ([]Hit, error)
	// ListSessions returns recently active sessions (by last indexed seq, then file mtime).
	ListSessions(ctx context.Context, limit int) ([]SessionSummary, error)
	// ScrollMessages returns messages around anchorSeq in one session (before/after counts).
	ScrollMessages(ctx context.Context, sessionID string, anchorSeq int64, before, after int) ([]MessageRow, error)
}
