package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/lengzhao/agentkit"
)

// StaticStore returns one Session for matching IDs. Useful for CLI presets and
// tests where a single session instance is wired through pluginkit.
type StaticStore struct {
	sess   agentkit.Session
	binds  map[agentkit.SessionID]agentkit.AgentID
	active map[agentkit.SessionID]agentkit.SessionID
	mu     sync.RWMutex
}

func NewStaticStore(sess agentkit.Session) *StaticStore {
	return &StaticStore{
		sess:   sess,
		binds:  make(map[agentkit.SessionID]agentkit.AgentID),
		active: make(map[agentkit.SessionID]agentkit.SessionID),
	}
}

func (s *StaticStore) Get(_ context.Context, id agentkit.SessionID) (agentkit.Session, error) {
	if s.sess == nil {
		return nil, fmt.Errorf("static session store has no session")
	}
	if id != "" && id != s.sess.ID() {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return s.sess, nil
}

func (s *StaticStore) AgentBind(_ context.Context, id agentkit.SessionID) (agentkit.AgentID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id == "" {
		id = s.sess.ID()
	}
	return s.binds[id], nil
}

func (s *StaticStore) SetAgentBind(_ context.Context, id agentkit.SessionID, agent agentkit.AgentID) error {
	if id == "" {
		id = s.sess.ID()
	}
	s.mu.Lock()
	s.binds[id] = agent
	s.mu.Unlock()
	return nil
}

func (s *StaticStore) ActiveSession(_ context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	if id == "" {
		id = s.sess.ID()
	}
	s.mu.RLock()
	active := s.active[id]
	s.mu.RUnlock()
	if active == "" {
		return id, nil
	}
	return active, nil
}

func (s *StaticStore) SetActiveSession(_ context.Context, id, active agentkit.SessionID) error {
	if id == "" {
		id = s.sess.ID()
	}
	if active == "" {
		return fmt.Errorf("active session id is required")
	}
	s.mu.Lock()
	s.active[id] = active
	s.mu.Unlock()
	return nil
}

type StaticConfig struct{}

type StaticDeps struct {
	Session agentkit.Session `json:"session"`
}

// NewStatic registers session/static: Wrap one pre-built Session as a store that returns it for every id.
//
// Best practices:
//   - For tests and single-session hosts; every session id maps to the same log.
func NewStatic(_ StaticConfig, deps StaticDeps) (agentkit.SessionStore, error) {
	if deps.Session == nil {
		return nil, fmt.Errorf("session/static requires session dependency")
	}
	return NewStaticStore(deps.Session), nil
}
