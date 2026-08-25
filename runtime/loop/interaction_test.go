package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestRunInteractionSyncPath(t *testing.T) {
	t.Parallel()

	ctrl := NewControl()
	var started, ended int
	emit := func(_ context.Context, out agentkit.OutboundEvent) error {
		switch out.Type {
		case agentkit.EventInteractionStart:
			started++
		case agentkit.EventInteractionEnd:
			ended++
		}
		return nil
	}
	handler := stubInteractionHandler{reply: agentkit.InteractionReply{Text: "2"}}

	ctx := withTurnContext(context.Background(), "cli:default", "coder", "cli", "", ctrl, emit, handler, false)
	result, err := ctrl.RunInteraction(ctx, agentkit.HumanInteraction{
		Kind:   agentkit.InteractionQuestion,
		Prompt: "pick one",
		Options: []agentkit.InteractionOption{
			{Label: "alpha"},
			{Label: "beta"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Answered || result.Text != "beta" || result.Selected != 1 {
		t.Fatalf("result = %+v", result)
	}
	if started != 1 || ended != 1 {
		t.Fatalf("started=%d ended=%d", started, ended)
	}
}

func TestRunInteractionHeadlessUnanswered(t *testing.T) {
	t.Parallel()

	ctrl := NewControl()
	emit := func(context.Context, agentkit.OutboundEvent) error { return nil }
	ctx := withTurnContext(context.Background(), "worker:job", "coder", "worker", "", ctrl, emit, nil, false)

	result, err := ctrl.RunInteraction(ctx, agentkit.HumanInteraction{
		Kind:   agentkit.InteractionQuestion,
		Prompt: "anyone there?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Answered || !strings.Contains(result.Reason, "no interactive user") {
		t.Fatalf("result = %+v", result)
	}
}

func TestTryDeliverInteractionConsumesReply(t *testing.T) {
	t.Parallel()

	loop, err := New(Config{DefaultAgent: "coder"}, Deps{Agents: []agentkit.Agent{stubAgent{}}})
	if err != nil {
		t.Fatal(err)
	}
	l := loop.(*Default)
	ctrl := l.controlFor("feishu:oc_test")
	ctrl.setPendingInteraction(&pendingInteraction{id: "ix1", ch: make(chan agentkit.InteractionReply, 1)})

	event := agentkit.MessageEvent{
		SessionID:          "feishu:oc_test",
		InteractionReplyTo: "ix1",
		Message:            agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "beta"}}},
	}
	if !l.TryDeliverInteraction(event) {
		t.Fatal("expected deliver to succeed")
	}
	select {
	case reply := <-ctrl.pending.ch:
		if reply.Text != "beta" {
			t.Fatalf("reply = %q", reply.Text)
		}
	default:
		t.Fatal("expected pending channel to receive reply")
	}
}

type stubInteractionHandler struct {
	reply agentkit.InteractionReply
	err   error
}

func (s stubInteractionHandler) ReadInteractionReply(context.Context, agentkit.HumanInteraction) (agentkit.InteractionReply, error) {
	return s.reply, s.err
}

type stubAgent struct{}

func (stubAgent) ID() agentkit.AgentID { return "coder" }

func (stubAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }
