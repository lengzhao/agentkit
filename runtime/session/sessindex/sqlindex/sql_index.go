package sqlindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lengzhao/agentkit"
	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessindex"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/pluginkit"
	_ "modernc.org/sqlite"
)

// SQLIndexConfig mirrors session/sql connection settings so the index lives in
// the same database as ak_session_events (one DSN to persist in Docker).
type SQLIndexConfig struct {
	// Driver is database/sql driver name: sqlite, pgx (postgres), or any registered driver.
	Driver string `json:"driver"`
	// DSN is the driver data source name; should match the session/sql store DSN.
	DSN string `json:"dsn"`
}

type SQLIndexDeps struct{}

// Validate implements pluginkit.Validator.
func (c *SQLIndexConfig) Validate() error {
	if strings.TrimSpace(c.Driver) == "" {
		return fmt.Errorf("session/sql-index driver is required")
	}
	if strings.TrimSpace(c.DSN) == "" {
		return fmt.Errorf("session/sql-index dsn is required")
	}
	return nil
}

// SQLIndex derives a searchable transcript index from session/sql durable events.
type SQLIndex struct {
	driver string
	db     *sql.DB
	mu     sync.Mutex
}

func init() {
	pluginkit.Register("session/sql-index", NewSQLIndex)
}

var _ capsessionindex.Service = (*SQLIndex)(nil)

// NewSQLIndex registers session/sql-index: FTS over session/sql durable events.
func NewSQLIndex(cfg SQLIndexConfig, _ SQLIndexDeps) (capsessionindex.Service, error) {
	db, err := sessstore.OpenSQLDB(cfg.Driver, cfg.DSN)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("session/sql-index ping: %w", err)
	}
	s := &SQLIndex{driver: sessstore.NormalizeSQLDriver(cfg.Driver), db: db}
	if err := s.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLIndex) isSQLite() bool { return s.driver == "sqlite" }

// tenantFilter returns a WHERE fragment scoping session ids to the caller's
// workspace: channel-scope sessions collapse to the workspace key itself,
// thread/user scopes append ":..." segments. Empty when global scope was
// requested or no workspace is known.
func tenantFilter(ctx context.Context, column string) (string, []any) {
	if rctx.SessionIndexGlobalFromContext(ctx) {
		return "", nil
	}
	wsKey := strings.TrimSpace(rctx.WorkspaceFromContext(ctx))
	if wsKey == "" {
		return "", nil
	}
	return fmt.Sprintf(" AND (%s = ? OR substr(%s, 1, ?) = ?)", column, column),
		[]any{wsKey, len(wsKey) + 1, wsKey + ":"}
}

// sessionInTenant reports whether sessionID belongs to the caller's workspace.
func sessionInTenant(ctx context.Context, sessionID string) bool {
	if rctx.SessionIndexGlobalFromContext(ctx) {
		return true
	}
	wsKey := strings.TrimSpace(rctx.WorkspaceFromContext(ctx))
	if wsKey == "" {
		return true
	}
	return sessionID == wsKey || strings.HasPrefix(sessionID, wsKey+":")
}

func (s *SQLIndex) q(query string) string {
	return sessstore.SQLPlaceholderQuery(s.driver, query)
}

func (s *SQLIndex) ensureSchema() error {
	var stmts []string
	if s.isSQLite() {
		stmts = []string{
			`CREATE TABLE IF NOT EXISTS ak_session_index_state (
				session_id TEXT PRIMARY KEY,
				max_seq INTEGER NOT NULL
			)`,
			`CREATE VIRTUAL TABLE IF NOT EXISTS ak_session_fts USING fts5(
				session_id UNINDEXED,
				seq UNINDEXED,
				role UNINDEXED,
				body,
				tokenize='porter'
			)`,
		}
	} else {
		stmts = []string{
			`CREATE TABLE IF NOT EXISTS ak_session_index_state (
				session_id TEXT PRIMARY KEY,
				max_seq BIGINT NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS ak_session_fts (
				session_id TEXT NOT NULL,
				seq BIGINT NOT NULL,
				role TEXT NOT NULL,
				body TEXT NOT NULL,
				PRIMARY KEY (session_id, seq)
			)`,
			`CREATE INDEX IF NOT EXISTS ak_session_fts_session_seq ON ak_session_fts (session_id, seq)`,
		}
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("session/sql-index schema: %w", err)
		}
	}
	// Migration: recency tracking for ListSessions ordering. Tolerate re-runs.
	if _, err := s.db.Exec(`ALTER TABLE ak_session_index_state ADD COLUMN max_created_at_ns BIGINT NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("session/sql-index migrate max_created_at_ns: %w", err)
		}
	}
	return nil
}

// SyncSessions ingests new events from ak_session_events above the per-session watermark.
func (s *SQLIndex) SyncSessions(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cond, condArgs := tenantFilter(ctx, "e.session_id")
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT e.session_id, e.seq, e.payload
		FROM ak_session_events e
		LEFT JOIN ak_session_index_state st ON st.session_id = e.session_id
		WHERE e.seq > COALESCE(st.max_seq, 0)`+cond+`
		ORDER BY e.session_id, e.seq`), condArgs...)
	if err != nil {
		return err
	}
	defer rows.Close()

	type pending struct {
		msgs         []sessindex.IndexableMessage
		maxSeq       int64
		maxCreatedNs int64
	}
	bySession := make(map[string]*pending)
	var order []string
	for rows.Next() {
		var sessionID string
		var seq int64
		var raw []byte
		if err := rows.Scan(&sessionID, &seq, &raw); err != nil {
			return err
		}
		var ev agentkit.SessionEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		p := bySession[sessionID]
		if p == nil {
			p = &pending{}
			bySession[sessionID] = p
			order = append(order, sessionID)
		}
		if seq > p.maxSeq {
			p.maxSeq = seq
		}
		if ns := ev.CreatedAt.UnixNano(); ns > p.maxCreatedNs {
			p.maxCreatedNs = ns
		}
		p.msgs = append(p.msgs, sessindex.ExtractIndexableMessages([]agentkit.SessionEvent{ev})...)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insert, err := tx.PrepareContext(ctx, s.q(
		`INSERT INTO ak_session_fts (session_id, seq, role, body) VALUES (?, ?, ?, ?)`))
	if err != nil {
		return err
	}
	defer insert.Close()

	upsertState := `INSERT INTO ak_session_index_state (session_id, max_seq, max_created_at_ns) VALUES (?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			max_seq = excluded.max_seq,
			max_created_at_ns = MAX(ak_session_index_state.max_created_at_ns, excluded.max_created_at_ns)`

	for _, sessionID := range order {
		p := bySession[sessionID]
		for _, m := range p.msgs {
			if _, err := insert.ExecContext(ctx, sessionID, m.Seq, m.Role, m.Text); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, s.q(upsertState), sessionID, p.maxSeq, p.maxCreatedNs); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLIndex) Search(ctx context.Context, query string, limit int) ([]capsessionindex.Hit, error) {
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
	s.mu.Lock()
	defer s.mu.Unlock()

	cond, condArgs := tenantFilter(ctx, "session_id")
	var rows *sql.Rows
	var err error
	if s.isSQLite() {
		escaped := strings.ReplaceAll(query, `"`, `""`)
		args := append([]any{`"` + escaped + `"`}, condArgs...)
		args = append(args, limit)
		rows, err = s.db.QueryContext(ctx, s.q(`
			SELECT session_id, seq, role, snippet(ak_session_fts, 3, '[', ']', '…', 32)
			FROM ak_session_fts
			WHERE ak_session_fts MATCH ?`+cond+`
			ORDER BY rank
			LIMIT ?`), args...)
	} else {
		args := append([]any{"%" + query + "%"}, condArgs...)
		args = append(args, limit)
		rows, err = s.db.QueryContext(ctx, s.q(`
			SELECT session_id, seq, role, substr(body, 1, 200)
			FROM ak_session_fts
			WHERE body LIKE ?`+cond+`
			ORDER BY session_id, seq
			LIMIT ?`), args...)
	}
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

func (s *SQLIndex) ListSessions(ctx context.Context, limit int) ([]capsessionindex.SessionSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cond, condArgs := tenantFilter(ctx, "m.session_id")
	where := " WHERE 1=1" + cond
	args := append(condArgs, limit)
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT m.session_id, COUNT(*), MAX(m.seq),
			COALESCE((
				SELECT st.max_created_at_ns FROM ak_session_index_state st
				WHERE st.session_id = m.session_id
			), 0) AS last_mod_ns
		FROM ak_session_fts m`+where+`
		GROUP BY m.session_id
		ORDER BY last_mod_ns DESC
		LIMIT ?`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []capsessionindex.SessionSummary
	for rows.Next() {
		var sum capsessionindex.SessionSummary
		var lastModNs int64
		if err := rows.Scan(&sum.SessionID, &sum.MessageCount, &sum.LastSeq, &lastModNs); err != nil {
			return nil, err
		}
		if lastModNs > 0 {
			sum.LastMod = time.Unix(0, lastModNs).UTC()
		}
		out = append(out, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Fallback for sessions synced before max_created_at_ns existed: latest
	// event time from the durable log.
	for i := range out {
		if !out[i].LastMod.IsZero() {
			continue
		}
		var raw []byte
		err := s.db.QueryRowContext(ctx, s.q(
			`SELECT payload FROM ak_session_events WHERE session_id = ? ORDER BY seq DESC LIMIT 1`),
			out[i].SessionID).Scan(&raw)
		if err != nil {
			continue
		}
		var ev agentkit.SessionEvent
		if err := json.Unmarshal(raw, &ev); err == nil && !ev.CreatedAt.IsZero() {
			out[i].LastMod = ev.CreatedAt.UTC()
		}
	}
	return out, nil
}

func (s *SQLIndex) ScrollMessages(ctx context.Context, sessionID string, anchorSeq int64, before, after int) ([]capsessionindex.MessageRow, error) {
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
	if !sessionInTenant(ctx, sessionID) {
		return nil, fmt.Errorf("session %q is outside the current workspace; hint: only sessions from the current chat/channel are accessible, use mode=list to see them", sessionID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if anchorSeq <= 0 {
		var maxSeq sql.NullInt64
		if err := s.db.QueryRowContext(ctx, s.q(
			`SELECT MAX(seq) FROM ak_session_fts WHERE session_id = ?`), sessionID).Scan(&maxSeq); err != nil {
			return nil, err
		}
		if maxSeq.Valid {
			anchorSeq = maxSeq.Int64
		}
	}
	var out []capsessionindex.MessageRow
	if before > 0 {
		rows, err := s.db.QueryContext(ctx, s.q(`
			SELECT seq, role, body FROM ak_session_fts
			WHERE session_id = ? AND seq < ?
			ORDER BY seq DESC
			LIMIT ?`), sessionID, anchorSeq, before)
		if err != nil {
			return nil, err
		}
		var prior []capsessionindex.MessageRow
		for rows.Next() {
			var row capsessionindex.MessageRow
			if err := rows.Scan(&row.Seq, &row.Role, &row.Text); err != nil {
				rows.Close()
				return nil, err
			}
			prior = append(prior, row)
		}
		rows.Close()
		for i := len(prior) - 1; i >= 0; i-- {
			out = append(out, prior[i])
		}
	}
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT seq, role, body FROM ak_session_fts
		WHERE session_id = ? AND seq >= ?
		ORDER BY seq ASC
		LIMIT ?`), sessionID, anchorSeq, after+1)
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
