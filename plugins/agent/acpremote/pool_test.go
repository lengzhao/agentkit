package acpremote

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

// countingPool builds a pool whose bridges are cheap shells: the tests assert
// pool bookkeeping, not ACP behaviour.
func countingPool(t *testing.T, cfg PoolConfig) (*bridgePool, *int32Counter) {
	t.Helper()
	counter := &int32Counter{}
	p, err := newBridgePool(cfg, func() *bridge {
		counter.add(1)
		// Mirror newBridge's lifecycle fields: closeBridge sends a detach op and
		// waits for it, and stop signals stopCh rather than closing connOps.
		b := &bridge{connOps: make(chan connOp), stopCh: make(chan struct{})}
		go b.connLoop()
		return b
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, counter
}

// int32Counter is a tiny concurrent counter (avoids importing sync/atomic into
// every assertion).
type int32Counter struct {
	mu sync.Mutex
	n  int
}

func (c *int32Counter) add(delta int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n += delta
}

func (c *int32Counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func TestPoolConfigDefaultsToTTL(t *testing.T) {
	t.Parallel()

	got, err := PoolConfig{}.normalize()
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != StrategyTTL {
		t.Fatalf("default strategy = %q, want ttl (warm connections are the existing behaviour)", got.Strategy)
	}
	if got.IdleTTL != defaultPoolIdleTTL || got.MaxIdle != defaultPoolMaxIdle {
		t.Fatalf("defaults = %+v", got)
	}
}

func TestPoolConfigRejectsUnknownStrategy(t *testing.T) {
	t.Parallel()

	if _, err := (PoolConfig{Strategy: "forever"}).normalize(); err == nil {
		t.Fatal("expected an error for an unknown strategy")
	}
}

// none must not cache: a stateless scheduler mints a session id per fire, so a
// cached bridge would be a leaked CLI process.
func TestStrategyNoneCachesNothing(t *testing.T) {
	t.Parallel()

	p, created := countingPool(t, PoolConfig{Strategy: StrategyNone})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		lease, err := p.Acquire(ctx, agentkit.SessionID("schedule:job:1"))
		if err != nil {
			t.Fatal(err)
		}
		if lease.Reused() {
			t.Fatal("strategy none must never reuse a connection")
		}
		if got := p.Stats().Live; got != 0 {
			t.Fatalf("cached connections = %d, want 0 while leased under none", got)
		}
		lease.Release()
	}
	if created.get() != 3 {
		t.Fatalf("bridges created = %d, want one per acquire", created.get())
	}
	if got := p.Stats().Live; got != 0 {
		t.Fatalf("leaked %d connections under strategy none", got)
	}
}

// ttl keeps the connection warm and reuses it, which is the behaviour every
// existing host depends on (a cold CLI start costs seconds).
func TestStrategyTTLReusesWarmConnection(t *testing.T) {
	t.Parallel()

	p, created := countingPool(t, PoolConfig{Strategy: StrategyTTL})
	ctx := context.Background()
	sid := agentkit.SessionID("lark:oc_demo")

	first, err := p.Acquire(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reused() {
		t.Fatal("the first acquire cannot be a reuse")
	}
	first.Release()

	second, err := p.Acquire(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	if !second.Reused() {
		t.Fatal("a warm connection must be reused under ttl")
	}
	if created.get() != 1 {
		t.Fatalf("created %d bridges, want 1", created.get())
	}
	if second.Bridge() != first.Bridge() {
		t.Fatal("the lease should hand back the same bridge")
	}
}

// A connection with an outstanding lease must never be reclaimed. This is the
// invariant that replaces the "is a turn in flight" heuristic.
func TestReapSkipsLeasedConnections(t *testing.T) {
	t.Parallel()

	p, _ := countingPool(t, PoolConfig{Strategy: StrategyTTL, IdleTTL: time.Minute})
	ctx := context.Background()
	sid := agentkit.SessionID("busy")

	lease, err := p.Acquire(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if n := p.reapIdle(time.Now().Add(time.Hour)); n != 0 {
		t.Fatalf("reclaimed %d connections with a live lease", n)
	}
	if got := p.Stats().Live; got != 1 {
		t.Fatalf("live = %d, want the leased connection kept", got)
	}
	lease.Release()

	// Released and idle past the TTL: now it goes.
	if n := p.reapIdle(time.Now().Add(time.Hour)); n != 1 {
		t.Fatalf("reclaimed %d idle connections, want 1", n)
	}
	if got := p.Stats().Live; got != 0 {
		t.Fatalf("live = %d after reap", got)
	}
}

func TestStrategyPinnedKeepsConnections(t *testing.T) {
	t.Parallel()

	p, _ := countingPool(t, PoolConfig{Strategy: StrategyPinned, IdleTTL: time.Minute})
	ctx := context.Background()
	lease, err := p.Acquire(ctx, agentkit.SessionID("pinned"))
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()

	if n := p.reapIdle(time.Now().Add(24 * time.Hour)); n != 0 {
		t.Fatalf("pinned reaped %d connections", n)
	}
	if got := p.Stats().Live; got != 1 {
		t.Fatalf("live = %d, want 1", got)
	}
}

// MaxIdle is a safety valve for every strategy, including pinned.
func TestMaxIdleBoundsCachedConnections(t *testing.T) {
	t.Parallel()

	p, _ := countingPool(t, PoolConfig{Strategy: StrategyPinned, MaxIdle: 2})
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c", "d"} {
		lease, err := p.Acquire(ctx, agentkit.SessionID(id))
		if err != nil {
			t.Fatal(err)
		}
		lease.Release()
	}
	if got := p.Stats().Live; got > 2+1 {
		// The cap allows the just-released entry; it is reclaimed on the next
		// release. Assert we never grow without bound.
		t.Fatalf("cached connections = %d, want roughly MaxIdle", got)
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	p, _ := countingPool(t, PoolConfig{Strategy: StrategyTTL})
	lease, err := p.Acquire(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	lease.Release() // must not double-decrement or panic
	if got := p.Stats().Leased; got != 0 {
		t.Fatalf("leased = %d after release", got)
	}
}

func TestAcquireAfterCloseFails(t *testing.T) {
	t.Parallel()

	p, _ := countingPool(t, PoolConfig{Strategy: StrategyTTL})
	p.Close()
	if _, err := p.Acquire(context.Background(), "s"); err == nil {
		t.Fatal("expected an error after Close")
	}
}

func TestAcquireHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	p, _ := countingPool(t, PoolConfig{Strategy: StrategyTTL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Acquire(ctx, "s"); err == nil {
		t.Fatal("expected context cancellation")
	}
}

func TestConcurrentAcquireRelease(t *testing.T) {
	t.Parallel()

	p, created := countingPool(t, PoolConfig{Strategy: StrategyNone, MaxIdle: 4})
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lease, err := p.Acquire(ctx, agentkit.SessionID("s"))
			if err != nil {
				t.Error(err)
				return
			}
			defer lease.Release()
		}(i)
	}
	wg.Wait()
	if created.get() != 32 {
		t.Fatalf("created %d, want 32 under none", created.get())
	}
	if got := p.Stats().Live; got != 0 {
		t.Fatalf("leaked %d connections", got)
	}
}

// A slash command and a turn must contend for the same session through leases.
// This is the invariant that replaced the old "is a turn in flight" guard: the
// connection cannot be reclaimed while either holds a lease.
func TestLeaseGuardsSlashCommandPath(t *testing.T) {
	t.Parallel()

	p, _ := countingPool(t, PoolConfig{Strategy: StrategyTTL, IdleTTL: time.Minute})
	ctx := context.Background()
	sid := agentkit.SessionID("lark:oc_demo")

	// A slash command takes a lease (same call acquireLease makes in production).
	slashLease, err := p.Acquire(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if slashLease.Reused() {
		t.Fatal("first acquire cannot be a reuse")
	}

	// A reaper sweep during the command must not reclaim it. Before the pool,
	// this was the hole: slash commands set no turn, so a currentTurn() guard
	// let the connection be closed under them.
	if n := p.reapIdle(time.Now().Add(time.Hour)); n != 0 {
		t.Fatalf("reclaimed a connection held by a slash command")
	}
	if got := p.Stats().Leased; got != 1 {
		t.Fatalf("leased = %d, want 1", got)
	}

	// A concurrent turn on the same session shares the bridge rather than
	// opening a second subprocess.
	turnLease, err := p.Acquire(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !turnLease.Reused() {
		t.Fatal("second acquire should reuse the warm bridge")
	}
	if turnLease.Bridge() != slashLease.Bridge() {
		t.Fatal("both leases must hand back the same bridge")
	}

	slashLease.Release()
	if got := p.Stats().Leased; got != 1 {
		t.Fatalf("leased = %d after one release, want the turn lease still held", got)
	}
	// Still guarded by the remaining lease.
	if n := p.reapIdle(time.Now().Add(time.Hour)); n != 0 {
		t.Fatalf("reclaimed while a turn lease was outstanding")
	}

	turnLease.Release()
	if n := p.reapIdle(time.Now().Add(time.Hour)); n != 1 {
		t.Fatalf("reclaimed %d after all leases released, want 1", n)
	}
	if got := p.Stats().Live; got != 0 {
		t.Fatalf("live = %d after reap", got)
	}
}
