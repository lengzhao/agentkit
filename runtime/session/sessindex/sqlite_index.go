package sessindex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
	"github.com/lengzhao/agentkit/cap/workspace"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	_ "modernc.org/sqlite"
)

type SQLiteIndexConfig struct {
	// IndexRel is the workspace-relative SQLite file path.
	IndexRel string `json:"indexRel"`
}

type SQLiteIndexDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

// SQLiteIndex persists FTS5 over session JSONL transcripts per tenant workspace.
type SQLiteIndex struct {
	workspace workspace.Service
	indexRel  string
	mu        sync.Mutex
}

// NewSQLiteIndex registers session/sqlite-index: FTS index over session/store JSONL files.
func NewSQLiteIndex(cfg SQLiteIndexConfig, deps SQLiteIndexDeps) (capsessionindex.Service, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("session/sqlite-index requires workspace")
	}
	rel := strings.TrimSpace(cfg.IndexRel)
	if rel == "" {
		rel = "sessions/.index.sqlite"
	}
	return &SQLiteIndex{
		workspace: deps.Workspace,
		indexRel:  rel,
	}, nil
}

func (s *SQLiteIndex) SyncSessions(ctx context.Context, sessionsDir string) error {
	if strings.TrimSpace(sessionsDir) == "" {
		return fmt.Errorf("sessions dir is required")
	}
	dbPath, err := s.workspace.Resolve(ctx, s.indexRel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		return err
	}
	defer db.Close()

	if err := s.ensureSchema(db); err != nil {
		return err
	}

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(sessionsDir, ent.Name())
		if err := s.syncOneFile(db, path); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteIndex) Search(ctx context.Context, query string, limit int) ([]capsessionindex.Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	dbPath, err := s.workspace.Resolve(ctx, s.indexRel)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureSchema(db); err != nil {
		return nil, err
	}

	// FTS5 phrase query; escape quotes loosely.
	escaped := strings.ReplaceAll(query, `"`, `""`)
	q := fmt.Sprintf(`"%s"`, escaped)
	rows, err := db.Query(`
		SELECT session_id, seq, role, snippet(messages, 0, '[', ']', '…', 32)
		FROM messages
		WHERE messages MATCH ?
		ORDER BY rank
		LIMIT ?`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []capsessionindex.Hit
	for rows.Next() {
		var h capsessionindex.Hit
		if err := rows.Scan(&h.SessionID, &h.Seq, &h.Role, &h.Snippet); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func (s *SQLiteIndex) ListSessions(ctx context.Context, limit int) ([]capsessionindex.SessionSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	dbPath, err := s.workspace.Resolve(ctx, s.indexRel)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureSchema(db); err != nil {
		return nil, err
	}
	rows, err := db.Query(`
		SELECT m.session_id,
			COUNT(*),
			MAX(m.seq),
			COALESCE((
				SELECT MAX(f.mtime_ns) FROM files f
				WHERE f.path LIKE '%/' || m.session_id || '.jsonl'
			), 0)
		FROM messages m
		GROUP BY m.session_id
		ORDER BY MAX(m.seq) DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []capsessionindex.SessionSummary
	for rows.Next() {
		var sum capsessionindex.SessionSummary
		var mtimeNs int64
		if err := rows.Scan(&sum.SessionID, &sum.MessageCount, &sum.LastSeq, &mtimeNs); err != nil {
			return nil, err
		}
		if mtimeNs > 0 {
			sum.LastMod = time.Unix(0, mtimeNs).UTC()
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

func (s *SQLiteIndex) ScrollMessages(ctx context.Context, sessionID string, anchorSeq int64, before, after int) ([]capsessionindex.MessageRow, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}
	if before == 0 && after == 0 {
		before = 3
		after = 3
	}
	dbPath, err := s.workspace.Resolve(ctx, s.indexRel)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureSchema(db); err != nil {
		return nil, err
	}
	if anchorSeq <= 0 {
		var maxSeq sql.NullInt64
		if err := db.QueryRow(`SELECT MAX(seq) FROM messages WHERE session_id = ?`, sessionID).Scan(&maxSeq); err != nil {
			return nil, err
		}
		if maxSeq.Valid {
			anchorSeq = maxSeq.Int64
		}
	}
	var out []capsessionindex.MessageRow
	if before > 0 {
		rows, err := db.Query(`
			SELECT seq, role, body FROM messages
			WHERE session_id = ? AND seq < ?
			ORDER BY seq DESC
			LIMIT ?`, sessionID, anchorSeq, before)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var prior []capsessionindex.MessageRow
		for rows.Next() {
			var row capsessionindex.MessageRow
			if err := rows.Scan(&row.Seq, &row.Role, &row.Text); err != nil {
				return nil, err
			}
			prior = append(prior, row)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for i := len(prior) - 1; i >= 0; i-- {
			out = append(out, prior[i])
		}
	}
	rows, err := db.Query(`
		SELECT seq, role, body FROM messages
		WHERE session_id = ? AND seq >= ?
		ORDER BY seq ASC
		LIMIT ?`, sessionID, anchorSeq, after+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row capsessionindex.MessageRow
		if err := rows.Scan(&row.Seq, &row.Role, &row.Text); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *SQLiteIndex) ensureSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS files (
			path TEXT PRIMARY KEY,
			mtime_ns INTEGER NOT NULL,
			max_seq INTEGER NOT NULL
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS messages USING fts5(
			session_id UNINDEXED,
			seq UNINDEXED,
			role UNINDEXED,
			body,
			tokenize='porter'
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteIndex) syncOneFile(db *sql.DB, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	mtime := info.ModTime().UnixNano()
	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")

	var storedMtime, storedSeq int64
	err = db.QueryRow(`SELECT mtime_ns, max_seq FROM files WHERE path = ?`, path).Scan(&storedMtime, &storedSeq)
	switch {
	case err == sql.ErrNoRows:
	case err != nil:
		return err
	default:
		if storedMtime == mtime {
			return nil
		}
	}

	events, maxSeq, _, err := sessstore.ScanSessionFile(path, 0)
	if err != nil {
		return err
	}
	msgs := ExtractIndexableMessages(events)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO messages(session_id, seq, role, body) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, m := range msgs {
		if _, err := stmt.Exec(sessionID, m.Seq, m.Role, m.Text); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE path = ?`, path); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO files(path, mtime_ns, max_seq) VALUES (?, ?, ?)`, path, mtime, int64(maxSeq)); err != nil {
		return err
	}
	return tx.Commit()
}
