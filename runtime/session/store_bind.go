package session

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/lengzhao/agentkit"
)

type bindCacheEntry struct {
	agentID agentkit.AgentID
	modTime time.Time
}

type activeCacheEntry struct {
	sessionID agentkit.SessionID
	modTime   time.Time
}

func (s *Store) AgentBind(ctx context.Context, id agentkit.SessionID) (agentkit.AgentID, error) {
	if id == "" {
		return "", fmt.Errorf("session id is required")
	}
	dir, err := s.storeDir(ctx)
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

	if v, ok := s.bindCache.Load(id); ok {
		entry := v.(bindCacheEntry)
		if entry.modTime.Equal(info.ModTime()) {
			return entry.agentID, nil
		}
	}

	agentID, err := readAgentBindFile(path)
	if err != nil {
		return "", err
	}
	s.bindCache.Store(id, bindCacheEntry{agentID: agentID, modTime: info.ModTime()})
	return agentID, nil
}

func (s *Store) SetAgentBind(ctx context.Context, id agentkit.SessionID, agent agentkit.AgentID) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	dir, err := s.storeDir(ctx)
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
	s.bindCache.Store(id, bindCacheEntry{agentID: agent, modTime: info.ModTime()})
	return nil
}

func (s *Store) ActiveSession(ctx context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	if id == "" {
		return "", fmt.Errorf("session id is required")
	}
	dir, err := s.storeDir(ctx)
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
	if v, ok := s.activeCache.Load(id); ok {
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
	s.activeCache.Store(id, activeCacheEntry{sessionID: active, modTime: info.ModTime()})
	return active, nil
}

func (s *Store) SetActiveSession(ctx context.Context, id, active agentkit.SessionID) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	if active == "" {
		return fmt.Errorf("active session id is required")
	}
	dir, err := s.storeDir(ctx)
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
	s.activeCache.Store(id, activeCacheEntry{sessionID: active, modTime: info.ModTime()})
	return nil
}

func (s *Store) storeDir(ctx context.Context) (string, error) {
	return ensureTenantLayout(ctx, s.workspace, s.relDir)
}
