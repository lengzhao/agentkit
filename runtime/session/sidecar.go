package session

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

// memorySidecar stores per-session agent binds and active-session mappings in memory.
type memorySidecar struct {
	mu       sync.RWMutex
	binds    map[agentkit.SessionID]agentkit.AgentID
	active   map[agentkit.SessionID]agentkit.SessionID
	fallback agentkit.SessionID
}

func newMemorySidecar(fallback agentkit.SessionID) memorySidecar {
	return memorySidecar{
		binds:    make(map[agentkit.SessionID]agentkit.AgentID),
		active:   make(map[agentkit.SessionID]agentkit.SessionID),
		fallback: fallback,
	}
}

func (m *memorySidecar) normalize(id agentkit.SessionID) agentkit.SessionID {
	if id == "" {
		return m.fallback
	}
	return id
}

func (m *memorySidecar) AgentBind(_ context.Context, id agentkit.SessionID) (agentkit.AgentID, error) {
	id = m.normalize(id)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.binds[id], nil
}

func (m *memorySidecar) SetAgentBind(_ context.Context, id agentkit.SessionID, agent agentkit.AgentID) error {
	id = m.normalize(id)
	m.mu.Lock()
	m.binds[id] = agent
	m.mu.Unlock()
	return nil
}

func (m *memorySidecar) ActiveSession(_ context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	id = m.normalize(id)
	m.mu.RLock()
	active := m.active[id]
	m.mu.RUnlock()
	if active == "" {
		return id, nil
	}
	return active, nil
}

func (m *memorySidecar) SetActiveSession(_ context.Context, id, active agentkit.SessionID) error {
	id = m.normalize(id)
	if active == "" {
		return fmt.Errorf("active session id is required")
	}
	m.mu.Lock()
	m.active[id] = active
	m.mu.Unlock()
	return nil
}

type bindCacheEntry struct {
	agentID agentkit.AgentID
	modTime time.Time
}

type activeCacheEntry struct {
	sessionID agentkit.SessionID
	modTime   time.Time
}

// fileSidecar persists agent binds and active-session mappings beside session logs.
type fileSidecar struct {
	dir         func(ctx context.Context) (string, error)
	bindCache   sync.Map
	activeCache sync.Map
}

func (f *fileSidecar) AgentBind(ctx context.Context, id agentkit.SessionID) (agentkit.AgentID, error) {
	if id == "" {
		return "", fmt.Errorf("session id is required")
	}
	dir, err := f.dir(ctx)
	if err != nil {
		return "", err
	}
	path, err := agentBindFilePath(dir, id)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	if v, ok := f.bindCache.Load(id); ok {
		entry := v.(bindCacheEntry)
		if entry.modTime.Equal(info.ModTime()) {
			return entry.agentID, nil
		}
	}

	agentID, err := readAgentBindFile(path)
	if err != nil {
		return "", err
	}
	f.bindCache.Store(id, bindCacheEntry{agentID: agentID, modTime: info.ModTime()})
	return agentID, nil
}

func (f *fileSidecar) SetAgentBind(ctx context.Context, id agentkit.SessionID, agent agentkit.AgentID) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	dir, err := f.dir(ctx)
	if err != nil {
		return err
	}
	path, err := agentBindFilePath(dir, id)
	if err != nil {
		return err
	}
	if err := writeAgentBindFile(path, agent); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	f.bindCache.Store(id, bindCacheEntry{agentID: agent, modTime: info.ModTime()})
	return nil
}

func (f *fileSidecar) ActiveSession(ctx context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	if id == "" {
		return "", fmt.Errorf("session id is required")
	}
	dir, err := f.dir(ctx)
	if err != nil {
		return "", err
	}
	path, err := activeSessionFilePath(dir, id)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return id, nil
		}
		return "", err
	}
	if v, ok := f.activeCache.Load(id); ok {
		entry := v.(activeCacheEntry)
		if entry.modTime.Equal(info.ModTime()) {
			return entry.sessionID, nil
		}
	}

	active, err := readActiveSessionFile(path)
	if err != nil {
		return "", err
	}
	if active == "" {
		active = id
	}
	f.activeCache.Store(id, activeCacheEntry{sessionID: active, modTime: info.ModTime()})
	return active, nil
}

func (f *fileSidecar) SetActiveSession(ctx context.Context, id, active agentkit.SessionID) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	if active == "" {
		return fmt.Errorf("active session id is required")
	}
	dir, err := f.dir(ctx)
	if err != nil {
		return err
	}
	path, err := activeSessionFilePath(dir, id)
	if err != nil {
		return err
	}
	if err := writeActiveSessionFile(path, active); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	f.activeCache.Store(id, activeCacheEntry{sessionID: active, modTime: info.ModTime()})
	return nil
}
