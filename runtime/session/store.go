package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type StoreConfig struct {
	Dir string `json:"dir"`
}

type StoreDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

// Store opens one JSONL session file per SessionID under Dir.
// Suitable for IM platforms where each channel/thread maps to a distinct session.
type Store struct {
	relDir    string
	workspace workspace.Service
	mu        sync.Mutex
	cache     map[agentkit.SessionID]*JSONL
}

func NewStore(cfg StoreConfig, deps StoreDeps) (agentkit.SessionStore, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("session store requires workspace")
	}
	if cfg.Dir == "" {
		return nil, fmt.Errorf("session store dir is required")
	}
	return &Store{
		relDir:    cfg.Dir,
		workspace: deps.Workspace,
		cache:     make(map[agentkit.SessionID]*JSONL),
	}, nil
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
	if sess, ok := s.cache[id]; ok {
		return sess, nil
	}
	path, err := sessionFilePath(dir, id)
	if err != nil {
		return nil, err
	}
	sess, err := newJSONL(JSONLConfig{Path: path, ID: id})
	if err != nil {
		return nil, err
	}
	s.cache[id] = sess
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
	return b.String() + ".jsonl", nil
}
