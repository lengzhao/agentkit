package sessbind_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestChannelAgentBindIsolatedByWorkspace(t *testing.T) {
	t.Parallel()

	ws, err := rtworkspace.NewTenant(rtworkspace.TenantConfig{
		Global:    t.TempDir(),
		LocalBase: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctxA := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Workspace: "slack:C001"})
	ctxB := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Workspace: "slack:C002"})

	if err := sessbind.SetChannelAgentBind(ctxA, ws, "worker"); err != nil {
		t.Fatal(err)
	}
	got, err := sessbind.ChannelAgentBind(ctxA, ws)
	if err != nil || got != "worker" {
		t.Fatalf("channel A = %q err=%v", got, err)
	}
	got, err = sessbind.ChannelAgentBind(ctxB, ws)
	if err != nil || got != "" {
		t.Fatalf("channel B = %q err=%v", got, err)
	}

	if err := sessbind.SetChannelAgentBind(ctxA, ws, ""); err != nil {
		t.Fatal(err)
	}
	got, err = sessbind.ChannelAgentBind(ctxA, ws)
	if err != nil || got != "" {
		t.Fatalf("cleared = %q err=%v", got, err)
	}
}
