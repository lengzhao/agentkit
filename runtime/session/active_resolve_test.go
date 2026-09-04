package session_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

type activeMapStore struct {
	active map[agentkit.SessionID]agentkit.SessionID
}

func (s activeMapStore) Get(_ context.Context, id agentkit.SessionID) (agentkit.Session, error) {
	return nil, nil
}

func (s activeMapStore) ActiveSession(_ context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	if active, ok := s.active[id]; ok {
		return active, nil
	}
	return id, nil
}

func (s activeMapStore) SetActiveSession(_ context.Context, id, active agentkit.SessionID) error {
	s.active[id] = active
	return nil
}

func TestResolveActiveSessionID(t *testing.T) {
	t.Parallel()

	entry := agentkit.SessionID("slack:C001")
	logical := agentkit.SessionID("slack:C001:new:20260829")
	store := activeMapStore{active: map[agentkit.SessionID]agentkit.SessionID{entry: logical}}

	got, err := session.ResolveActiveSessionID(context.Background(), store, entry)
	if err != nil {
		t.Fatal(err)
	}
	if got != logical {
		t.Fatalf("got %q want %q", got, logical)
	}

	got, err = session.ResolveActiveSessionID(context.Background(), store, agentkit.SessionID("cli:default"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "cli:default" {
		t.Fatalf("got %q want cli:default", got)
	}
}
