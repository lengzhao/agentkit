package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type StoreConfig struct {
	// Dir is root directory holding one file per session, resolved through the workspace.
	Dir string `json:"dir"`
	// MaxCachedSessions limits in-memory hot sessions (LRU). Zero keeps every opened session cached.
	MaxCachedSessions int `json:"maxCachedSessions"`
	// CacheIdleTTL evicts sessions unused for this duration (for example "30m"). Empty disables idle eviction.
	CacheIdleTTL string `json:"cacheIdleTTL"`
	// MaxLoadedEvents limits non-compaction events kept in memory per session on load. Zero loads the full file.
	MaxLoadedEvents int `json:"maxLoadedEvents"`
}

type StoreDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

// Store opens one JSONL session file per SessionID under Dir.
// Suitable for IM platforms where each channel/thread maps to a distinct session.
type Store struct {
	relDir              string
	workspace           workspace.Service
	maxLoadedEvents     int
	mu                  sync.Mutex
	cache               sessionCache
	bindCache           sync.Map // SessionID -> bindCacheEntry
	activeCache         sync.Map // SessionID -> activeCacheEntry
}

// NewStore registers session/store: Resolve durable JSONL sessions by id; contributes /new and /session.
func NewStore(cfg StoreConfig, deps StoreDeps) (agentkit.SessionStore, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("session store requires workspace")
	}
	if cfg.Dir == "" {
		return nil, fmt.Errorf("session store dir is required")
	}
	if cfg.MaxCachedSessions < 0 {
		return nil, fmt.Errorf("session store maxCachedSessions must be >= 0")
	}
	if cfg.MaxLoadedEvents < 0 {
		return nil, fmt.Errorf("session store maxLoadedEvents must be >= 0")
	}
	idleTTL, err := parseCacheIdleTTL(cfg.CacheIdleTTL)
	if err != nil {
		return nil, err
	}
	s := &Store{
		relDir:          cfg.Dir,
		workspace:       deps.Workspace,
		maxLoadedEvents: cfg.MaxLoadedEvents,
		cache:               newSessionCache(cfg.MaxCachedSessions, idleTTL, time.Now),
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

func parseCacheIdleTTL(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("session store cacheIdleTTL: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("session store cacheIdleTTL must be positive")
	}
	return d, nil
}

func (s *Store) runCacheJanitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		s.cache.evict(s.cache.now())
		s.mu.Unlock()
	}
}

func (s *Store) Get(ctx context.Context, id agentkit.SessionID) (agentkit.Session, error) {
	if id == "" {
		return nil, fmt.Errorf("session id is required")
	}
	dir, err := s.workspace.Resolve(ctx, s.relDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.cache.get(id); ok {
		return sess, nil
	}
	path, err := sessionFilePath(dir, id)
	if err != nil {
		return nil, err
	}
	sess, err := newJSONL(JSONLConfig{
		Path:            path,
		ID:              id,
		MaxLoadedEvents: s.maxLoadedEvents,
	})
	if err != nil {
		return nil, err
	}
	s.cache.put(id, sess)
	return sess, nil
}

func sessionFilePath(dir string, id agentkit.SessionID) (string, error) {
	name, err := safeSessionFileName(id)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absPath, absDir+string(os.PathSeparator)) && absPath != absDir {
		return "", fmt.Errorf("session path escapes store dir")
	}
	return absPath, nil
}

func safeSessionFileName(id agentkit.SessionID) (string, error) {
	base, err := safeSessionName(id)
	if err != nil {
		return "", err
	}
	return base + ".jsonl", nil
}

func safeSessionName(id agentkit.SessionID) (string, error) {
	raw := string(id)
	if raw == "" {
		return "", fmt.Errorf("empty session id")
	}
	if strings.Contains(raw, "..") {
		return "", fmt.Errorf("invalid session id")
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String(), nil
}
