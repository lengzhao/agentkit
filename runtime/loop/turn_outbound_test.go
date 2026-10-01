package loop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

func TestWrapTurnEndNoticesEmitsStepLimitBeforeTurnEnd(t *testing.T) {
	t.Parallel()

	var order []agentkit.EventType
	emit := func(_ context.Context, event agentkit.OutboundEvent) error {
		order = append(order, event.Type)
		return nil
	}
	wrapped := wrapTurnEndNotices(emit)

	endData := capsession.TurnEndData{
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

func TestStampTurnIDTagsEveryEvent(t *testing.T) {
	t.Parallel()

	var got []string
	emit := func(_ context.Context, event agentkit.OutboundEvent) error {
		got = append(got, event.TurnID)
		return nil
	}
	wrapped := stampTurnID(emit, "turn-1")
	for _, typ := range []agentkit.EventType{agentkit.EventTurnStart, agentkit.EventMessageUpdate, agentkit.EventTurnEnd} {
		if err := wrapped(context.Background(), agentkit.OutboundEvent{Type: typ}); err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range got {
		if id != "turn-1" {
			t.Fatalf("event %d TurnID = %q, want turn-1", i, id)
		}
	}
}

func TestStampTurnIDNilEmit(t *testing.T) {
	t.Parallel()
	if stampTurnID(nil, "turn-1") != nil {
		t.Fatal("stampTurnID(nil) should return nil")
	}
}

func TestTurnIDFromMetadata(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		meta map[string]any
		want string
	}{
		{"nil metadata", nil, ""},
		{"missing key", map[string]any{"other": "x"}, ""},
		{"non-string value", map[string]any{agentkit.MetadataTurnID: 42}, ""},
		{"blank value", map[string]any{agentkit.MetadataTurnID: "  "}, ""},
		{"valid value", map[string]any{agentkit.MetadataTurnID: " turn-9 "}, "turn-9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := turnIDFromMetadata(tc.meta); got != tc.want {
				t.Fatalf("turnIDFromMetadata = %q, want %q", got, tc.want)
			}
		})
	}
}
