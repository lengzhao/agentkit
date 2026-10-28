package acpremote

import (
	"context"
	"testing"
	"time"
)

func TestStoppedBridgeCleanupDoesNotBlock(t *testing.T) {
	b := newBridge(Config{}, nil, nil, nil, nil, nil, nil, nil)
	b.stop()
	done := make(chan struct{})
	go func() { b.releaseSubprocess(); _ = b.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup blocked after connection loop stopped")
	}
}

func TestPoolCloseWaitsForLastLease(t *testing.T) {
	p, _ := countingPool(t, PoolConfig{})
	first, err := p.Acquire(context.Background(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Acquire(context.Background(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	first.Release()
	select {
	case <-second.Bridge().stopCh:
		t.Fatal("closed bridge while another lease was held")
	default:
	}
	second.Release()
	select {
	case <-second.Bridge().stopCh:
	default:
		t.Fatal("bridge was not closed after final release")
	}
}
