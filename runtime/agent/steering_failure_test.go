package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/loop"
)

func TestSteeringPersistenceFailureRejectsCurrentAndPreservesRest(t *testing.T) {
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	var outcomes []string
	for _, text := range []string{"first", "second"} {
		err := ctrl.SteerEntry(t.Context(), agentkit.SteerEntry{
			Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: text}}},
			Ack:     func(outcome string) { outcomes = append(outcomes, outcome) },
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	wantErr := errors.New("storage unavailable")
	a := &Runtime{id: "test"}
	_, err := a.runSegment(t.Context(), steeringFailedSession{err: wantErr}, nil, ctrl, &turnRun{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("runSegment error = %v", err)
	}
	if len(outcomes) != 1 || outcomes[0] != capsession.SteerOutcomeRejected {
		t.Fatalf("outcomes = %v, want [rejected]", outcomes)
	}
	remaining := ctrl.PopSteering()
	if len(remaining) != 1 || remaining[0].Message.Content[0].Text != "second" {
		t.Fatalf("unprocessed entries lost: %v", remaining)
	}
}

type steeringFailedSession struct {
	agentkit.Session
	err error
}

func (s steeringFailedSession) Append(context.Context, agentkit.SessionEvent) (agentkit.EventSeq, error) {
	return 0, s.err
}
