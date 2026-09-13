package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

// memorySidecar stores per-session agent binds and active-session mappings in memory.
type memorySidecar struct {
	mu       sync.RWMutex
	runtime  map[agentkit.SessionID]SessionRuntimeData
	active   map[agentkit.SessionID]agentkit.SessionID
	fallback agentkit.SessionID
}

func newMemorySidecar(fallback agentkit.SessionID) memorySidecar {
	return memorySidecar{
		runtime:  make(map[agentkit.SessionID]SessionRuntimeData),
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

func (m *memorySidecar) sessionRuntime(id agentkit.SessionID) SessionRuntimeData {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.runtime[id]
}

func (m *memorySidecar) AgentBind(_ context.Context, id agentkit.SessionID) (agentkit.AgentID, error) {
	return m.sessionRuntime(m.normalize(id)).AgentID, nil
}

func (m *memorySidecar) SetAgentBind(_ context.Context, id agentkit.SessionID, agent agentkit.AgentID) error {
	id = m.normalize(id)
	agent = agentkit.AgentID(strings.TrimSpace(string(agent)))
	m.mu.Lock()
	data := m.runtime[id]
	data.AgentID = agent
	m.storeRuntime(id, data)
	m.mu.Unlock()
	return nil
}

func (m *memorySidecar) ModelBind(_ context.Context, id agentkit.SessionID) (string, error) {
	return m.sessionRuntime(m.normalize(id)).Model, nil
}

func (m *memorySidecar) SetModelBind(_ context.Context, id agentkit.SessionID, model string) error {
	id = m.normalize(id)
	model = strings.TrimSpace(model)
	m.mu.Lock()
	data := m.runtime[id]
	data.Model = model
	m.storeRuntime(id, data)
	m.mu.Unlock()
	return nil
}

func (m *memorySidecar) storeRuntime(id agentkit.SessionID, data SessionRuntimeData) {
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(data.AgentID)))
	data.Model = strings.TrimSpace(data.Model)
	if data.AgentID == "" && data.Model == "" {
		delete(m.runtime, id)
		return
	}
	m.runtime[id] = data
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

type runtimeCacheEntry struct {
	data    SessionRuntimeData
	modTime time.Time
	missing bool
}

type activeCacheEntry struct {
	sessionID agentkit.SessionID
	modTime   time.Time
}

// fileSidecar persists agent binds and active-session mappings beside session logs.
type fileSidecar struct {
	dir           func(ctx context.Context) (string, error)
	runtimeCache  sync.Map
	activeCache   sync.Map
}

func (f *fileSidecar) loadSessionRuntimeCached(ctx context.Context, id agentkit.SessionID) (SessionRuntimeData, error) {
	dir, err := f.dir(ctx)
	if err != nil {
		return SessionRuntimeData{}, err
	}
	path, err := sessionRuntimeFilePath(dir, id)
	if err != nil {
		return SessionRuntimeData{}, err
	}
	info, statErr := os.Stat(path)
	if statErr == nil {
		if v, ok := f.runtimeCache.Load(id); ok {
			entry := v.(runtimeCacheEntry)
			if !entry.missing && entry.modTime.Equal(info.ModTime()) {
				return entry.data, nil
			}
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		if v, ok := f.runtimeCache.Load(id); ok {
			entry := v.(runtimeCacheEntry)
			if entry.missing {
				return entry.data, nil
			}
		}
	} else {
		return SessionRuntimeData{}, statErr
	}
	data, err := loadSessionRuntime(dir, id)
	if err != nil {
		return SessionRuntimeData{}, err
	}
	modTime := time.Time{}
	missing := errors.Is(statErr, os.ErrNotExist)
	if statErr == nil {
		modTime = info.ModTime()
	}
	f.runtimeCache.Store(id, runtimeCacheEntry{data: data, modTime: modTime, missing: missing})
	return data, nil
}

func (f *fileSidecar) AgentBind(ctx context.Context, id agentkit.SessionID) (agentkit.AgentID, error) {
	if id == "" {
		return "", fmt.Errorf("session id is required")
	}
	data, err := f.loadSessionRuntimeCached(ctx, id)
	if err != nil {
		return "", err
	}
	return data.AgentID, nil
}

func (f *fileSidecar) SetAgentBind(ctx context.Context, id agentkit.SessionID, agent agentkit.AgentID) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	dir, err := f.dir(ctx)
	if err != nil {
		return err
	}
	if err := setSessionRuntimeAgent(dir, id, agent); err != nil {
		return err
	}
	f.runtimeCache.Delete(id)
	return nil
}

func (f *fileSidecar) ModelBind(ctx context.Context, id agentkit.SessionID) (string, error) {
	if id == "" {
		return "", fmt.Errorf("session id is required")
	}
	data, err := f.loadSessionRuntimeCached(ctx, id)
	if err != nil {
		return "", err
	}
	return data.Model, nil
}

func (f *fileSidecar) SetModelBind(ctx context.Context, id agentkit.SessionID, model string) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	dir, err := f.dir(ctx)
	if err != nil {
		return err
	}
	if err := setSessionRuntimeModel(dir, id, model); err != nil {
		return err
	}
	f.runtimeCache.Delete(id)
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
