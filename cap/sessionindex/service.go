package sessionindex

import "context"

// Hit is one FTS match in a session transcript.
type Hit struct {
	SessionID string
	Seq       int64
	Role      string
	Snippet   string
}

// Service indexes durable session JSONL logs and supports full-text search per tenant workspace.
type Service interface {
	// SyncSessions ingests or refreshes index entries from a sessions directory.
	SyncSessions(ctx context.Context, sessionsDir string) error
	// Search runs FTS over indexed messages for the current workspace database.
	Search(ctx context.Context, query string, limit int) ([]Hit, error)
}
