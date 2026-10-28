package acpremote

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
)

// peekBridge returns the bridge currently cached for a session, without taking a
// lease. Test-only: production code must hold a lease, or the pool may reclaim
// the connection underneath it.
//
// Several slash-command and steering tests assert on the subprocess that a
// *completed* command left behind, so they inspect state rather than hold a
// connection. A lease would change what the assertion is about (a lease keeps
// the bridge alive, which is exactly the condition under test elsewhere).
func (a *Runtime) peekBridge(sessionID agentkit.SessionID) *bridge {
	if a.pool == nil {
		return nil
	}
	a.pool.mu.Lock()
	defer a.pool.mu.Unlock()
	if entry := a.pool.entries[sessionID]; entry != nil {
		return entry.bridge
	}
	return nil
}

// bridgeCount reports cached connections.
func (a *Runtime) bridgeCount() int {
	if a.pool == nil {
		return 0
	}
	return a.pool.Stats().Live
}

// requireBridge fails when a session has no cached bridge, so a test that
// expected the command to connect cannot silently pass on nil.
func requireBridge(t *testing.T, a *Runtime, sessionID agentkit.SessionID) *bridge {
	t.Helper()
	b := a.peekBridge(sessionID)
	if b == nil {
		t.Fatalf("session %q has no cached bridge", sessionID)
	}
	return b
}

// releaseSessionBridge detaches the subprocess for a session, mirroring what a
// host does between turns when it wants a cold reconnect.
func releaseSessionBridge(t *testing.T, a *Runtime, sessionID agentkit.SessionID) {
	t.Helper()
	if b := a.peekBridge(sessionID); b != nil {
		b.releaseSubprocess()
	}
}

// acquireTestLease takes a lease the caller must release; it exists so tests
// exercise the same path production uses.
func acquireTestLease(t *testing.T, a *Runtime, sessionID agentkit.SessionID) *bridgeLease {
	t.Helper()
	lease, err := a.acquireLease(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("acquire lease for %q: %v", sessionID, err)
	}
	return lease
}
