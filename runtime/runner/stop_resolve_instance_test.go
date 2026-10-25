package runner

import (
	"context"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"testing"
)

func TestStopCandidatesStayInAdapterInstance(t *testing.T) {
	route := rctx.SessionRoute("slack", "slack:C1").WithInstance("slack1")
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), rctx.ResolveEnvelope(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: route}}, rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel)))
	ids, err := stopCandidateSessionIDs(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == "slack:C1" {
			t.Fatalf("cross-instance candidate: %v", ids)
		}
	}
}
