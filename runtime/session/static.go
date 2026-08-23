package session

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
)

// StaticStore returns one Session for matching IDs. Useful for CLI presets and
// tests where a single session instance is wired through pluginkit.
type StaticStore struct {
	sess agentkit.Session
}

func NewStaticStore(sess agentkit.Session) *StaticStore {
	return &StaticStore{sess: sess}
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

type StaticConfig struct{}

type StaticDeps struct {
	Session agentkit.Session `json:"session"`
}

func NewStatic(_ StaticConfig, deps StaticDeps) (agentkit.SessionStore, error) {
	if deps.Session == nil {
		return nil, fmt.Errorf("session/static requires session dependency")
	}
	return NewStaticStore(deps.Session), nil
}
