package subagent

import (
	"testing"
	"time"

	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
)

func TestDelegationWallClockPerCallWins(t *testing.T) {
	t.Parallel()
	secs := 42
	got, err := DelegationWallClock(capsubagent.Request{TimeoutSeconds: &secs}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != 42*time.Second {
		t.Fatalf("got %v want 42s", got)
	}
}

func TestDelegationWallClockUsesConfiguredWhenOmitted(t *testing.T) {
	t.Parallel()
	got, err := DelegationWallClock(capsubagent.Request{}, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got != 5*time.Minute {
		t.Fatalf("got %v", got)
	}
}

func TestDelegationWallClockRejectsNegative(t *testing.T) {
	t.Parallel()
	neg := -1
	if _, err := DelegationWallClock(capsubagent.Request{TimeoutSeconds: &neg}, time.Minute); err == nil {
		t.Fatal("expected error")
	}
}
