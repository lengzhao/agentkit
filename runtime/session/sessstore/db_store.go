package sessstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type SQLStoreConfig struct {
	// Driver is database/sql driver name: sqlite, pgx (postgres), or any registered driver.
	Driver string `json:"driver"`
	// DSN is the driver data source name (postgres URL, sqlite file URI, etc.).
	DSN string `json:"dsn"`
	// MaxCachedSessions limits in-memory hot sessions (LRU). Zero keeps every opened session cached.
	MaxCachedSessions int `json:"maxCachedSessions"`
	// CacheIdleTTL evicts sessions unused for this duration (for example "30m"). Empty disables idle eviction.
	CacheIdleTTL string `json:"cacheIdleTTL"`
	// MaxLoadedEvents limits non-compaction events kept in memory per session on load. Zero loads the full log.
	MaxLoadedEvents int `json:"maxLoadedEvents"`
}

type SQLStoreDeps struct{}

// SQLStore persists sessions in a SQL database instead of workspace JSONL files.
type SQLStore struct {
	driver          string
	db              *sql.DB
	maxLoadedEvents int
	mu              sync.Mutex
	cache           sessionCache
}

// NewSQLStore registers session/sql: durable sessions in SQLite/Postgres (or any database/sql backend).
func NewSQLStore(cfg SQLStoreConfig, _ SQLStoreDeps) (agentkit.SessionStore, error) {
	if cfg.MaxCachedSessions < 0 {
		return nil, fmt.Errorf("session sql maxCachedSessions must be >= 0")
	}
	if cfg.MaxLoadedEvents < 0 {
		return nil, fmt.Errorf("session sql maxLoadedEvents must be >= 0")
	}
	idleTTL, err := parseCacheIdleTTL(cfg.CacheIdleTTL)
	if err != nil {
		return nil, err
	}
	db, err := OpenSQLDB(cfg.Driver, cfg.DSN)
	if err != nil {
		return nil, err
	}
	driver := NormalizeSQLDriver(cfg.Driver)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("session sql ping: %w", err)
	}
	if err := ensureSessionSQLSchema(db, driver); err != nil {
		db.Close()
		return nil, err
	}
	s := &SQLStore{
		driver:          driver,
		db:              db,
		maxLoadedEvents: cfg.MaxLoadedEvents,
		cache:           newSessionCache(cfg.MaxCachedSessions, idleTTL, time.Now),
	}
	if idleTTL > 0 {
		interval := idleTTL / 2
		if interval < time.Minute {
			interval = time.Minute
		}
		go s.runCacheJanitor(interval)
	}
	return s, nil
}

// NewSQLiteStore registers session/sqlite: same as session/sql with driver sqlite.
func NewSQLiteStore(cfg SQLStoreConfig, deps SQLStoreDeps) (agentkit.SessionStore, error) {
	if strings.TrimSpace(cfg.Driver) == "" {
		cfg.Driver = "sqlite"
	}
	return NewSQLStore(cfg, deps)
}

// NewPostgresStore registers session/postgres: same as session/sql with driver pgx.
func NewPostgresStore(cfg SQLStoreConfig, deps SQLStoreDeps) (agentkit.SessionStore, error) {
	if strings.TrimSpace(cfg.Driver) == "" {
		cfg.Driver = "pgx"
	}
	return NewSQLStore(cfg, deps)
}

func (s *SQLStore) runCacheJanitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		s.cache.evict(s.cache.now())
		s.mu.Unlock()
	}
}

func (s *SQLStore) Get(ctx context.Context, id agentkit.SessionID) (agentkit.Session, error) {
	if id == "" {
		return nil, fmt.Errorf("session id is required")
	}
	s.mu.Lock()
	if sess, ok := s.cache.get(id); ok {
		s.mu.Unlock()
		return sess, nil
	}
	s.mu.Unlock()

	sess, err := newDBSession(s.db, s.driver, id, s.maxLoadedEvents)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.cache.get(id); ok {
		return existing, nil
	}
	s.cache.put(id, sess)
	return sess, nil
}

func (s *SQLStore) query(ctx context.Context, q string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, SQLPlaceholderQuery(s.driver, q), args...)
}

func (s *SQLStore) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, SQLPlaceholderQuery(s.driver, q), args...)
}

func (s *SQLStore) AgentBind(ctx context.Context, id agentkit.SessionID) (agentkit.AgentID, error) {
	if id == "" {
		return "", fmt.Errorf("session id is required")
	}
	var agent string
	err := s.query(ctx, `SELECT agent_id FROM ak_session_runtime WHERE stable_id = ?`, string(id)).Scan(&agent)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return agentkit.AgentID(strings.TrimSpace(agent)), nil
}

func (s *SQLStore) SetAgentBind(ctx context.Context, id agentkit.SessionID, agent agentkit.AgentID) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	agent = agentkit.AgentID(strings.TrimSpace(string(agent)))
	var model string
	err := s.query(ctx, `SELECT model FROM ak_session_runtime WHERE stable_id = ?`, string(id)).Scan(&model)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	model = strings.TrimSpace(model)
	if agent == "" && model == "" {
		_, err := s.exec(ctx, `DELETE FROM ak_session_runtime WHERE stable_id = ?`, string(id))
		return err
	}
	_, err = s.exec(ctx,
		`INSERT INTO ak_session_runtime (stable_id, agent_id, model) VALUES (?, ?, ?)
		 ON CONFLICT(stable_id) DO UPDATE SET agent_id = excluded.agent_id`,
		string(id), string(agent), model,
	)
	return err
}

func (s *SQLStore) ModelBind(ctx context.Context, id agentkit.SessionID) (string, error) {
	if id == "" {
		return "", fmt.Errorf("session id is required")
	}
	var model string
	err := s.query(ctx, `SELECT model FROM ak_session_runtime WHERE stable_id = ?`, string(id)).Scan(&model)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(model), nil
}

func (s *SQLStore) SetModelBind(ctx context.Context, id agentkit.SessionID, model string) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	model = strings.TrimSpace(model)
	var agent string
	err := s.query(ctx, `SELECT agent_id FROM ak_session_runtime WHERE stable_id = ?`, string(id)).Scan(&agent)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	agent = strings.TrimSpace(agent)
	if agent == "" && model == "" {
		_, err := s.exec(ctx, `DELETE FROM ak_session_runtime WHERE stable_id = ?`, string(id))
		return err
	}
	_, err = s.exec(ctx,
		`INSERT INTO ak_session_runtime (stable_id, agent_id, model) VALUES (?, ?, ?)
		 ON CONFLICT(stable_id) DO UPDATE SET model = excluded.model`,
		string(id), agent, model,
	)
	return err
}

func (s *SQLStore) ActiveSession(ctx context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	if id == "" {
		return "", fmt.Errorf("session id is required")
	}
	var active string
	err := s.query(ctx, `SELECT active_session_id FROM ak_session_active WHERE stable_id = ?`, string(id)).Scan(&active)
	if err == sql.ErrNoRows {
		return id, nil
	}
	if err != nil {
		return "", err
	}
	active = strings.TrimSpace(active)
	if active == "" {
		return id, nil
	}
	return agentkit.SessionID(active), nil
}

func (s *SQLStore) SetActiveSession(ctx context.Context, id, active agentkit.SessionID) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	if active == "" {
		return fmt.Errorf("active session id is required")
	}
	_, err := s.exec(ctx,
		`INSERT INTO ak_session_active (stable_id, active_session_id) VALUES (?, ?)
		 ON CONFLICT(stable_id) DO UPDATE SET active_session_id = excluded.active_session_id`,
		string(id), string(active),
	)
	return err
}
