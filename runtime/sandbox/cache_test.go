package sandbox

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestViewCacheHitSameView(t *testing.T) {
	s, _, _ := newTestSandbox(t)
	ctx := context.Background()

	v1, err := s.render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v1 != v2 {
		t.Fatal("second render within TTL must return cached *view")
	}
}

func TestViewCacheExpiredRefreshes(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	ctx := context.Background()

	v1, err := s.render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.viewMu.Lock()
	s.views[local] = cachedView{v: v1, exp: time.Now().Add(-time.Second)}
	s.viewMu.Unlock()

	v2, err := s.render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v1 == v2 {
		t.Fatal("expired cache entry must be re-rendered")
	}
}

func TestViewCacheMaxEvictsExpired(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	ctx := context.Background()
	now := time.Now()
	s.views = make(map[string]cachedView, viewCacheMax)
	for i := 0; i < viewCacheMax; i++ {
		key := fmt.Sprintf("/expired/tenant-%d", i)
		s.views[key] = cachedView{
			v:   &view{tenantRoot: key},
			exp: now.Add(-time.Minute),
		}
	}
	if _, err := s.render(ctx); err != nil {
		t.Fatal(err)
	}
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	if len(s.views) != 1 {
		t.Fatalf("expected only fresh tenant entry, got %d", len(s.views))
	}
	if _, ok := s.views[local]; !ok {
		t.Fatalf("cache must key on tenant root %q", local)
	}
}

func TestViewCacheMaxResetsWhenAllLive(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	ctx := context.Background()
	future := time.Now().Add(time.Hour)
	s.views = make(map[string]cachedView, viewCacheMax)
	for i := 0; i < viewCacheMax; i++ {
		key := fmt.Sprintf("/live/tenant-%d", i)
		s.views[key] = cachedView{
			v:   &view{tenantRoot: key},
			exp: future,
		}
	}
	if _, err := s.render(ctx); err != nil {
		t.Fatal(err)
	}
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	if len(s.views) != 1 {
		t.Fatalf("at capacity with no expired entries map must reset, got %d entries", len(s.views))
	}
	if _, ok := s.views[local]; !ok {
		t.Fatalf("after reset cache must hold current tenant %q", local)
	}
}

func TestViewCacheConcurrent(t *testing.T) {
	s, local, _ := newTestSandbox(t)
	ctx := context.Background()
	path := local + "/work/x.go"

	var wg sync.WaitGroup
	const n = 32
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			if err := s.CheckRead(ctx, path); err != nil {
				t.Errorf("CheckRead: %v", err)
			}
		}()
	}
	wg.Wait()
}
