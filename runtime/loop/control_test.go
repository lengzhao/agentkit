package loop_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/loop"
)

func TestDrainFollowUpsModes(t *testing.T) {
	t.Parallel()

	ctrl := loop.NewControl()
	ctx := context.Background()
	msg := func(text string) agentkit.ModelMessage {
		return agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		}
	}
	if err := ctrl.FollowUp(ctx, msg("one")); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.FollowUp(ctx, msg("two")); err != nil {
		t.Fatal(err)
	}

	one, err := ctrl.DrainFollowUps(ctx, agentkit.FollowUpOneAtATime)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].Content[0].Text != "one" {
		t.Fatalf("one-at-a-time drain = %+v", one)
	}

	rest, err := ctrl.DrainFollowUps(ctx, agentkit.FollowUpAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 1 || rest[0].Content[0].Text != "two" {
		t.Fatalf("remaining follow-ups = %+v", rest)
	}
}
