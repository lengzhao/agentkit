package sessstore

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// NormalizeSQLDriver maps driver aliases to canonical database/sql driver names.
func NormalizeSQLDriver(driver string) string {
	driver = strings.TrimSpace(strings.ToLower(driver))
	switch driver {
	case "postgres", "pgx":
		return "pgx"
	case "sqlite":
		return "sqlite"
	default:
		return driver
	}
}

// SQLPlaceholderQuery rewrites ? placeholders to $N for non-sqlite drivers.
func SQLPlaceholderQuery(driver, query string) string {
	if NormalizeSQLDriver(driver) == "sqlite" {
		return query
	}
	var b strings.Builder
	n := 1
	for _, r := range query {
		if r == '?' {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			n++
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// OpenSQLDB opens a database/sql handle for session storage backends.
func OpenSQLDB(driver, dsn string) (*sql.DB, error) {
	driver = NormalizeSQLDriver(driver)
	if driver == "" {
		return nil, fmt.Errorf("session sql driver is required")
	}
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("session sql dsn is required")
	}
	switch driver {
	case "sqlite":
		if err := ensureSQLiteFileParentDir(dsn); err != nil {
			return nil, err
		}
		if !strings.Contains(dsn, "_pragma=") {
			sep := "?"
			if strings.Contains(dsn, "?") {
				sep = "&"
			}
			dsn = dsn + sep + "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
		}
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	return db, nil
}

// ensureSQLiteFileParentDir creates parent directories for file-backed SQLite DSNs.
func ensureSQLiteFileParentDir(dsn string) error {
	if strings.Contains(dsn, "mode=memory") {
		return nil
	}
	path := dsn
	if strings.HasPrefix(path, "file:") {
		path = strings.TrimPrefix(path, "file:")
	}
	if path == "" || path == ":memory:" {
		return nil
	}
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" || dir == "/" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func ensureSessionSQLSchema(db *sql.DB, driver string) error {
	payloadType := "BLOB"
	if NormalizeSQLDriver(driver) == "pgx" {
		payloadType = "BYTEA"
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS ak_session_events (
			session_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			payload %s NOT NULL,
			PRIMARY KEY (session_id, seq)
		)`, payloadType),
		`CREATE INDEX IF NOT EXISTS ak_session_events_session_seq ON ak_session_events (session_id, seq)`,
		`CREATE TABLE IF NOT EXISTS ak_session_runtime (
			stable_id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS ak_session_active (
			stable_id TEXT PRIMARY KEY,
			active_session_id TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("session sql schema: %w", err)
		}
	}
	return nil
}
