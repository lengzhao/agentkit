package loop_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type stubAgent struct {
	id      agentkit.AgentID
	runTurn func(context.Context, agentkit.TurnInput) error
}

func (a stubAgent) ID() agentkit.AgentID { return a.id }
func (a stubAgent) RunTurn(ctx context.Context, input agentkit.TurnInput) error {
	if a.runTurn != nil {
		return a.runTurn(ctx, input)
	}
	return nil
}

func TestDispatchRecordsTelemetryTurn(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	l, err := loop.New(loop.Config{}, loop.Deps{
		Agents:    []agentkit.Agent{stubAgent{id: "coder"}},
		Telemetry: rec,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = l.Dispatch(context.Background(), agenttest.LoopRequest("cli:default", agentkit.MessageEvent{
		AgentID:    "coder",
		PlatformID: "cli",
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	}))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	turns, _, _ := rec.Snapshot()
	if len(turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(turns))
	}
	if turns[0].Meta.SessionID != "cli:default" {
		t.Fatalf("session id = %q", turns[0].Meta.SessionID)
	}
	if turns[0].Meta.Input == "" {
		t.Fatal("expected turn input summary")
	}
}
