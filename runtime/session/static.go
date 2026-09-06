package session

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
)

// StaticStore returns one Session for matching IDs. Useful for CLI presets and
// tests where a single session instance is wired through pluginkit.
type StaticStore struct {
	sess    agentkit.Session
	sidecar memorySidecar
}

func NewStaticStore(sess agentkit.Session) *StaticStore {
	fallback := agentkit.SessionID("")
	if sess != nil {
		fallback = sess.ID()
	}
	return &StaticStore{
		sess:    sess,
		sidecar: newMemorySidecar(fallback),
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

func (s *StaticStore) AgentBind(ctx context.Context, id agentkit.SessionID) (agentkit.AgentID, error) {
	return s.sidecar.AgentBind(ctx, id)
}

func (s *StaticStore) SetAgentBind(ctx context.Context, id agentkit.SessionID, agent agentkit.AgentID) error {
	return s.sidecar.SetAgentBind(ctx, id, agent)
}

func (s *StaticStore) ActiveSession(ctx context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	return s.sidecar.ActiveSession(ctx, id)
}

func (s *StaticStore) SetActiveSession(ctx context.Context, id, active agentkit.SessionID) error {
	return s.sidecar.SetActiveSession(ctx, id, active)
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
