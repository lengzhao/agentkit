package prompt

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestFrozenMemoryCachePerTurn(t *testing.T) {
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:default"})
	ctx = context.WithValue(ctx, agentkit.KeyTurnID, "turn-1")
	calls := 0
	load := func() (string, error) {
		calls++
		return "snapshot", nil
	}
	a, err := loadFrozenMemory(ctx, load)
	if err != nil || a != "snapshot" {
		t.Fatalf("first: %v %q", err, a)
	}
	b, err := loadFrozenMemory(ctx, load)
	if err != nil || b != "snapshot" || calls != 1 {
		t.Fatalf("second: calls=%d err=%v b=%q", calls, err, b)
	}
}
