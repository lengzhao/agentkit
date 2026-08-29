package session

import (
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

func TestSessionCacheLRUEvictsOldest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	clock := now
	cache := newSessionCache(2, 0, func() time.Time { return clock })

	s1 := &JSONL{id: "s1"}
	s2 := &JSONL{id: "s2"}
	s3 := &JSONL{id: "s3"}

	cache.put(agentkit.SessionID("s1"), s1)
	clock = clock.Add(time.Second)
	cache.put(agentkit.SessionID("s2"), s2)
	clock = clock.Add(time.Second)
	cache.put(agentkit.SessionID("s3"), s3)

	if cache.len() != 2 {
		t.Fatalf("cache len = %d, want 2", cache.len())
	}
	if _, ok := cache.get(agentkit.SessionID("s1")); ok {
		t.Fatal("expected s1 to be evicted")
	}
	if got, ok := cache.get(agentkit.SessionID("s2")); !ok || got != s2 {
		t.Fatalf("s2 = %+v, ok=%v", got, ok)
	}
	if got, ok := cache.get(agentkit.SessionID("s3")); !ok || got != s3 {
		t.Fatalf("s3 = %+v, ok=%v", got, ok)
	}
}

func TestSessionCacheTouchRefreshesLRU(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	clock := now
	cache := newSessionCache(2, 0, func() time.Time { return clock })

	s1 := &JSONL{id: "s1"}
	s2 := &JSONL{id: "s2"}
	s3 := &JSONL{id: "s3"}

	cache.put(agentkit.SessionID("s1"), s1)
	clock = clock.Add(time.Second)
	cache.put(agentkit.SessionID("s2"), s2)
	clock = clock.Add(time.Second)
	if _, ok := cache.get(agentkit.SessionID("s1")); !ok {
		t.Fatal("expected s1 in cache")
	}
	clock = clock.Add(time.Second)
	cache.put(agentkit.SessionID("s3"), s3)

	if _, ok := cache.get(agentkit.SessionID("s2")); ok {
		t.Fatal("expected s2 to be evicted after s1 was touched")
	}
}

func TestSessionCacheEvictsIdle(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	clock := start
	cache := newSessionCache(0, 30*time.Minute, func() time.Time { return clock })

	s1 := &JSONL{id: "s1"}
	cache.put(agentkit.SessionID("s1"), s1)
	clock = start.Add(31 * time.Minute)
	cache.evict(clock)

	if cache.len() != 0 {
		t.Fatalf("cache len = %d, want 0 after idle eviction", cache.len())
	}
}

func TestParseCacheIdleTTL(t *testing.T) {
	t.Parallel()

	if _, err := parseCacheIdleTTL(""); err != nil {
		t.Fatal(err)
	}
	d, err := parseCacheIdleTTL("30m")
	if err != nil || d != 30*time.Minute {
		t.Fatalf("30m = %v, err=%v", d, err)
	}
	if _, err := parseCacheIdleTTL("nope"); err == nil {
		t.Fatal("expected error for invalid duration")
	}
	if _, err := parseCacheIdleTTL("0s"); err == nil {
		t.Fatal("expected error for zero duration")
	}
}
