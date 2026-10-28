package permission_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	cappermission "github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/permission"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type recordingApprovalBroker struct{ envelope agentkit.TurnEnvelope }

func (b *recordingApprovalBroker) Await(ctx context.Context, _ cappermission.Request) (cappermission.Result, error) {
	b.envelope = rctx.EnvelopeFromContext(ctx)
	return cappermission.Result{Outcome: cappermission.OutcomeResolved}, nil
}

func TestParentBrokerCanWrapAnotherParentBroker(t *testing.T) {
	root := &recordingApprovalBroker{}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "root", Workspace: "tenant"})
	ctx = context.WithValue(ctx, agentkit.KeySessionControl, root)
	first := permission.NewParentBroker(ctx, "first")
	ctx = context.WithValue(rctx.WithConversation(ctx, "first-history"), agentkit.KeySessionControl, first)
	second := permission.NewParentBroker(ctx, "second")
	if _, err := second.Await(ctx, cappermission.Request{}); err != nil {
		t.Fatal(err)
	}
	if root.envelope.Conversation != "root" || root.envelope.AgentID != "second" {
		t.Fatalf("nested proxy lost leaf identity: %+v", root.envelope)
	}
}
