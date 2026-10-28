package acpremote

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

// Pool strategies. The default is strategyTTL: it preserves the warm-connection
// behaviour hosts already rely on (a cold `npx` start costs seconds) while
// bounding how many connections stay alive.
const (
	// strategyNone opens a connection for a lease and discards it on release.
	// Nothing is cached, so a caller that mints a new session id per turn (a
	// stateless scheduler) cannot accumulate processes.
	strategyNone = "none"
	// strategyTTL keeps up to MaxIdle connections, reclaiming one that has been
	// idle for IdleTTL. Interactive hosts want this: a warm CLI turns a
	// multi-second cold start into a single prompt round trip.
	strategyTTL = "ttl"
)

// PoolConfig is the connection-lifetime policy, configured per agent instance.
type PoolConfig struct {
	// Strategy is ttl (default) or none.
	Strategy string
	// IdleTTL is how long a cached connection may sit unused before it is
	// reclaimed. Only used by ttl. Defaults to 30 minutes.
	IdleTTL time.Duration
	// MaxIdle caps cached connections for the ttl strategy.
	// Defaults to 16.
	MaxIdle int
}

const (
	defaultPoolIdleTTL = 30 * time.Minute
	defaultPoolMaxIdle = 16
)

// normalize fills defaults and validates the strategy.
func (c PoolConfig) normalize() (PoolConfig, error) {
	switch strings.ToLower(strings.TrimSpace(c.Strategy)) {
	case "", strategyTTL:
		c.Strategy = strategyTTL
	case strategyNone:
		c.Strategy = strategyNone
	default:
		return c, fmt.Errorf("unknown pool strategy %q: use none or ttl", c.Strategy)
	}
	if c.IdleTTL <= 0 {
		c.IdleTTL = defaultPoolIdleTTL
	}
	if c.MaxIdle <= 0 {
		c.MaxIdle = defaultPoolMaxIdle
	}
	return c, nil
}

// bridgeLease keeps a session bridge alive for a turn or slash command.
// Multiple callers may hold leases; session/config mutexes serialize operations.
// Reclamation waits until all callers release their leases.
type bridgeLease struct {
	bridge *bridge
	once   sync.Once
	end    func()
}

// Bridge exposes the bridge to the caller. It stays valid until Release.
func (l *bridgeLease) Bridge() *bridge { return l.bridge }

// Release returns the lease to the pool. Idempotent: a deferred release after an
// explicit one does nothing.
func (l *bridgeLease) Release() {
	if l == nil || l.end == nil {
		return
	}
	l.once.Do(l.end)
}

// poolEntry is one cached bridge plus its bookkeeping.
type poolEntry struct {
	bridge   *bridge
	leases   int       // outstanding leases; never evict while > 0
	lastUsed time.Time // refreshed on release
}

// bridgePool owns every live bridge. All bridge creation and destruction goes
// through it, so there is exactly one place that can close a bridge's
// connection loop - and it can only do so when no lease is outstanding.
type bridgePool struct {
	cfg PoolConfig

	// newBridgeFn is injectable for tests.
	newBridgeFn func() *bridge

	mu      sync.Mutex
	entries map[agentkit.SessionID]*poolEntry
	closed  bool

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func newBridgePool(cfg PoolConfig, newBridgeFn func() *bridge) (*bridgePool, error) {
	normalized, err := cfg.normalize()
	if err != nil {
		return nil, err
	}
	p := &bridgePool{
		cfg:         normalized,
		newBridgeFn: newBridgeFn,
		entries:     make(map[agentkit.SessionID]*poolEntry),
		stopCh:      make(chan struct{}),
	}
	if normalized.Strategy == strategyTTL {
		p.wg.Add(1)
		go p.reapLoop()
	}
	return p, nil
}

// Acquire reserves the session bridge, creating it when the
// strategy caches nothing or nothing is cached yet.
func (p *bridgePool) Acquire(ctx context.Context, sessionID agentkit.SessionID) (*bridgeLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("acp-remote: pool is closed")
	}
	entry := p.entries[sessionID]
	if entry == nil {
		b := p.newBridgeFn()
		entry = &poolEntry{bridge: b}
		// strategyNone stores nothing: the bridge lives exactly as long as the
		// lease, so a per-turn session id cannot leak.
		if p.cfg.Strategy != strategyNone {
			p.entries[sessionID] = entry
		}
	}
	entry.leases++
	p.mu.Unlock()

	lease := &bridgeLease{bridge: entry.bridge}
	lease.end = func() { p.release(sessionID, entry) }
	return lease, nil
}

// release returns one lease. It destroys the bridge when the strategy keeps
// nothing, and otherwise keeps it warm until the reaper or the cap evicts it.
func (p *bridgePool) release(sessionID agentkit.SessionID, entry *poolEntry) {
	p.mu.Lock()
	if entry.leases > 0 {
		entry.leases--
	}
	entry.lastUsed = time.Now()

	// Only the final lease can retire the bridge, including after Close.
	discard := entry.leases == 0 && (p.closed || p.cfg.Strategy == strategyNone || p.overCapLocked())
	if discard && p.entries[sessionID] == entry {
		delete(p.entries, sessionID)
	}
	p.mu.Unlock()

	if discard {
		p.closeBridge(entry.bridge)
	}
}

// overCapLocked reports whether cached idle entries exceed MaxIdle. Must be
// called with p.mu held.
func (p *bridgePool) overCapLocked() bool {
	idle := 0
	for _, e := range p.entries {
		if e.leases == 0 {
			idle++
		}
	}
	return idle > p.cfg.MaxIdle
}

// closeBridge detaches the subprocess and retires the connection loop. Safe
// only when the pool holds no lease on the bridge.
func (p *bridgePool) closeBridge(b *bridge) {
	if b == nil {
		return
	}
	// detach first so no new work starts, then retire the goroutine. Both are
	// required: releasing without stop() leaks the conn loop and its channel.
	b.releaseSubprocess()
	b.stop()
}

// reapLoop reclaims idle ttl entries. It is the only background goroutine the
// pool runs, and it exits on Close.
func (p *bridgePool) reapLoop() {
	defer p.wg.Done()
	interval := p.cfg.IdleTTL / 2
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.reapIdle(time.Now())
		}
	}
}

// reapIdle destroys every cached bridge that has been idle past IdleTTL and has
// no outstanding lease. Returns how many were reclaimed.
func (p *bridgePool) reapIdle(now time.Time) int {
	if p.cfg.Strategy != strategyTTL {
		return 0
	}
	var stale []*bridge
	p.mu.Lock()
	for id, entry := range p.entries {
		if entry.leases > 0 {
			continue // a lease is in flight: never yank it
		}
		if now.Sub(entry.lastUsed) < p.cfg.IdleTTL {
			continue
		}
		delete(p.entries, id)
		stale = append(stale, entry.bridge)
	}
	p.mu.Unlock()

	for _, b := range stale {
		p.closeBridge(b)
	}
	if len(stale) > 0 {
		slog.Info("acp-remote: reclaimed idle connections",
			"count", len(stale), "idle_ttl", p.cfg.IdleTTL.String(), "strategy", p.cfg.Strategy)
	}
	return len(stale)
}

// poolStats reports cached bridge state for diagnostics.
type poolStats struct {
	Strategy string
	Live     int
	Leased   int
	Idle     int
	IdleTTL  time.Duration
	MaxIdle  int
}

// Stats returns a snapshot of pool state.
func (p *bridgePool) Stats() poolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := poolStats{Strategy: p.cfg.Strategy, IdleTTL: p.cfg.IdleTTL, MaxIdle: p.cfg.MaxIdle}
	for _, e := range p.entries {
		s.Live++
		if e.leases > 0 {
			s.Leased++
		} else {
			s.Idle++
		}
	}
	return s
}

// Close destroys every cached bridge and stops the reaper. Leases still
// outstanding are left alone; their own Release disposes of the bridge.
func (p *bridgePool) Close() {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		bridges := make([]*bridge, 0, len(p.entries))
		for id, e := range p.entries {
			delete(p.entries, id)
			if e.leases == 0 {
				bridges = append(bridges, e.bridge)
			}
		}
		p.mu.Unlock()

		close(p.stopCh)
		for _, b := range bridges {
			go p.closeBridge(b)
		}
	})
	p.wg.Wait()
}
