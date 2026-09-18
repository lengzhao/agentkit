package sessevents_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type reentrantGuardStore struct {
	inner  agentkit.SessionStore
	active sync.Map
}

func newReentrantGuardStore(inner agentkit.SessionStore) *reentrantGuardStore {
	return &reentrantGuardStore{inner: inner}
}

func (g *reentrantGuardStore) markTurnOpen(id agentkit.SessionID) {
	g.active.Store(id, struct{}{})
}

func (g *reentrantGuardStore) Get(ctx context.Context, id agentkit.SessionID) (agentkit.Session, error) {
	if _, ok := g.active.Load(id); ok {
		return nil, fmt.Errorf("session store: reentrant Get while turn open (%s)", id)
	}
	return g.inner.Get(ctx, id)
}

func TestParentSessionForDelegatePrefersOpenSession(t *testing.T) {
	t.Parallel()

	inner, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	guard := newReentrantGuardStore(inner)

	ctx := context.Background()
	const parentID = agentkit.SessionID("cli:default")
	open, err := inner.Get(ctx, parentID)
	if err != nil {
		t.Fatal(err)
	}
	guard.markTurnOpen(parentID)

	ctx = rctx.WithSession(ctx, open)
	got, err := sessevents.ParentSessionForDelegate(ctx, guard, parentID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != parentID {
		t.Fatalf("session id = %q", got.ID())
	}
}

func TestLoadSessionPrefersOpenTurn(t *testing.T) {
	t.Parallel()

	inner, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	guard := newReentrantGuardStore(inner)

	const id = agentkit.SessionID("cli:default")
	open, err := inner.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	guard.markTurnOpen(id)

	ctx := rctx.WithSession(context.Background(), open)
	got, err := sessevents.LoadSession(ctx, guard, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != id {
		t.Fatalf("id = %q", got.ID())
	}
}

func TestParentSessionForDelegateReentrantGetFailsWithoutOpenSession(t *testing.T) {
	t.Parallel()

	inner, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	guard := newReentrantGuardStore(inner)

	const parentID = agentkit.SessionID("cli:default")
	guard.markTurnOpen(parentID)

	_, err = sessevents.ParentSessionForDelegate(context.Background(), guard, parentID)
	if err == nil {
		t.Fatal("expected reentrant Get error")
	}
}
