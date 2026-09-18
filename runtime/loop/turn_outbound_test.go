package loop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

func TestWrapTurnEndNoticesEmitsStepLimitBeforeTurnEnd(t *testing.T) {
	t.Parallel()

	var order []agentkit.EventType
	emit := func(_ context.Context, event agentkit.OutboundEvent) error {
		order = append(order, event.Type)
		return nil
	}
	wrapped := wrapTurnEndNotices(emit)

	endData := sessevents.TurnEndData{
		Steps:      2,
		StopReason: string(agentkit.StopStepLimit),
		StepLimit:  2,
	}
	raw, err := json.Marshal(endData)
	if err != nil {
		t.Fatal(err)
	}
	if err := wrapped(context.Background(), agentkit.OutboundEvent{
		Type: agentkit.EventTurnEnd,
		Data: raw,
	}); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 {
		t.Fatalf("event order = %v, want assistant then turn/end", order)
	}
	if order[0] != agentkit.EventAssistantMessage || order[1] != agentkit.EventTurnEnd {
		t.Fatalf("event order = %v", order)
	}
}
