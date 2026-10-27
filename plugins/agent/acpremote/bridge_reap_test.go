package acpremote

import (
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

func newReapTestRuntime(t *testing.T, ttl time.Duration) *Runtime {
	t.Helper()
	rt, err := New(Config{
		ID:                   "cursor",
		Command:              []string{"/nonexistent/agent-acp-test-binary"},
		BridgeIdleTTLSeconds: int(ttl / time.Second),
	}, Deps{
		Workspace: &stubWorkspace{},
		FS:        bindTestFS(t, t.TempDir()),
		Telemetry: mustToolkit(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	a, ok := rt.(*Runtime)
	if !ok {
		t.Fatalf("New returned %T", rt)
	}
	return a
}

// A stateless scheduler mints a new session id per fire. Without reclamation
// every fire left a cached bridge (and, with a long-lived CLI, a process)
// behind forever.
func TestIdleBridgesAreEvicted(t *testing.T) {
	a := newReapTestRuntime(t, time.Minute)
	a.bridgesMu.Lock()
	a.bridgeTTL = time.Minute
	a.bridgesMu.Unlock()

	a.bridgeFor("schedule:job-1:1")
	a.bridgeFor("schedule:job-1:2")
	if got := a.bridgeCount(); got != 2 {
		t.Fatalf("bridges = %d, want 2", got)
	}

	// Fresh bridges survive a sweep.
	if n := a.evictIdleBridges(time.Now()); n != 0 {
		t.Fatalf("evicted %d fresh bridges", n)
	}

	// Two idle minutes: both go.
	if n := a.evictIdleBridges(time.Now().Add(2 * time.Minute)); n != 2 {
		t.Fatalf("evicted %d, want 2", n)
	}
	if got := a.bridgeCount(); got != 0 {
		t.Fatalf("bridges after sweep = %d, want 0", got)
	}
}

// A bridge with a turn in flight must never be released under the turn.
func TestBusyBridgeSurvivesEviction(t *testing.T) {
	a := newReapTestRuntime(t, time.Minute)
	a.bridgesMu.Lock()
	a.bridgeTTL = time.Minute
	a.bridgesMu.Unlock()

	b := a.bridgeFor("schedule:busy:1")
	b.setTurn(turnState{})
	if n := a.evictIdleBridges(time.Now().Add(time.Hour)); n != 0 {
		t.Fatalf("evicted %d busy bridges", n)
	}
	if got := a.bridgeCount(); got != 1 {
		t.Fatalf("bridges = %d, want 1", got)
	}
	b.clearTurn()
	if n := a.evictIdleBridges(time.Now().Add(time.Hour)); n != 1 {
		t.Fatalf("evicted %d idle bridges, want 1", n)
	}
}

// A negative TTL disables reclamation entirely (the old behaviour, opt-in).
func TestIdleTTLDisabledKeepsBridges(t *testing.T) {
	a := newReapTestRuntime(t, -time.Second)
	a.bridgesMu.Lock()
	a.bridgeTTL = -time.Second
	a.bridgesMu.Unlock()

	a.bridgeFor("s:1")
	if n := a.evictIdleBridges(time.Now().Add(24 * time.Hour)); n != 0 {
		t.Fatalf("evicted %d with reclamation disabled", n)
	}
	if got := a.bridgeCount(); got != 1 {
		t.Fatalf("bridges = %d, want 1", got)
	}
}

func TestBridgeIdleTTLDefault(t *testing.T) {
	cfg := Config{Command: []string{"x"}}
	cfg.SetDefaults()
	if cfg.BridgeIdleTTLSeconds != int(defaultBridgeIdleTTL/time.Second) {
		t.Fatalf("default TTL = %d", cfg.BridgeIdleTTLSeconds)
	}
}

func TestBridgeIdleTTLExplicit(t *testing.T) {
	cfg := Config{Command: []string{"x"}, BridgeIdleTTLSeconds: -1}
	cfg.SetDefaults()
	if cfg.BridgeIdleTTLSeconds != -1 {
		t.Fatalf("explicit TTL overwritten: %d", cfg.BridgeIdleTTLSeconds)
	}
}

var _ = strings.TrimSpace
var _ agentkit.SessionID
