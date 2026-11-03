package common

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestKeyedExecutorOrdersEachKeyAndRunsOtherKeys(t *testing.T) {
	e := NewKeyedExecutor(2, 8)
	defer e.Stop(context.Background())
	first := make(chan struct{})
	release := make(chan struct{})
	second := make(chan struct{})
	other := make(chan struct{})
	if err := e.Submit("chat", func(context.Context) { close(first); <-release }); err != nil {
		t.Fatal(err)
	}
	<-first
	_ = e.Submit("chat", func(context.Context) { close(second) })
	_ = e.Submit("other", func(context.Context) { close(other) })
	select {
	case <-other:
	case <-time.After(time.Second):
		t.Fatal("another chat was blocked")
	}
	select {
	case <-second:
		t.Fatal("same chat overtook its predecessor")
	default:
	}
	close(release)
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("same chat did not resume")
	}
}
func TestKeyedExecutorQueueIsBoundedAndAdmissionDoesNotBlock(t *testing.T) {
	e := NewKeyedExecutor(1, 1)
	release := make(chan struct{})
	started := make(chan struct{})
	_ = e.Submit("chat", func(context.Context) { close(started); <-release })
	<-started
	_ = e.Submit("chat", func(context.Context) {})
	if err := e.Submit("other", func(context.Context) {}); !errors.Is(err, ErrExecutorFull) {
		t.Fatalf("err=%v", err)
	}
	close(release)
	if err := e.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestKeyedExecutorHostCancellationStopsAcceptedWork(t *testing.T) {
	e := NewKeyedExecutor(1, 8)
	host, cancel := context.WithCancel(context.Background())
	e.Bind(host)
	started := make(chan struct{})
	canceled := make(chan struct{})
	var mu sync.Mutex
	ranQueued := false
	_ = e.Submit("chat", func(ctx context.Context) { close(started); <-ctx.Done(); close(canceled) })
	<-started
	_ = e.Submit("chat", func(context.Context) { mu.Lock(); ranQueued = true; mu.Unlock() })
	cancel()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := e.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("active task was not canceled")
	}
	mu.Lock()
	defer mu.Unlock()
	if ranQueued {
		t.Fatal("queued work ran after shutdown")
	}
	if err := e.Submit("new", func(context.Context) {}); !errors.Is(err, ErrExecutorStopped) {
		t.Fatalf("err=%v", err)
	}
}
func TestMessageDedupAdmissionFailureCanBeRetried(t *testing.T) {
	d := &MessageDedup{}
	full := errors.New("full")
	duplicate, err := d.Admit("message", func() error { return full })
	if duplicate || !errors.Is(err, full) {
		t.Fatal(duplicate, err)
	}
	accepted := 0
	duplicate, err = d.Admit("message", func() error { accepted++; return nil })
	if duplicate || err != nil || accepted != 1 {
		t.Fatal(duplicate, err, accepted)
	}
	duplicate, err = d.Admit("message", func() error { accepted++; return nil })
	if !duplicate || err != nil || accepted != 1 {
		t.Fatal(duplicate, err, accepted)
	}
}
